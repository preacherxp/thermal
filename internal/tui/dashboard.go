package tui

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"thermal-cli/internal/thermal"
)

type Config struct {
	Mode, Target       string
	Duration, StopTemp float64
}
type Result struct {
	Run   thermal.Run
	Saved string
	Err   error
}
type Work func(context.Context, func(string, thermal.Sample)) Result
type sampleMsg struct {
	phase  string
	sample thermal.Sample
}
type readyMsg struct{}
type tickMsg struct{}
type card struct {
	name, state, device, score string
	seconds                    float64
	temp, power, clock         *float64
	throttled                  *bool
	trace                      []*float64
	notes                      string
}
type model struct {
	cfg           Config
	renderer      *lipgloss.Renderer
	spin          spinner.Model
	bar           progress.Model
	width, height int
	cards         []card
	phase         string
	cancel        context.CancelFunc
	ctx           context.Context
	work          Work
	started       bool
	stopping      bool
	events        chan sampleMsg
	completed     chan Result
	result        *Result
}

func newModel(ctx context.Context, cancel context.CancelFunc, cfg Config, out io.Writer, work Work) *model {
	renderer := lipgloss.NewRenderer(out)
	// The interactive path has already verified ANSI support.
	if renderer.ColorProfile() == termenv.Ascii {
		renderer.SetColorProfile(termenv.ANSI256)
	}
	s := spinner.New(spinner.WithSpinner(spinner.Dot))
	s.Style = renderer.NewStyle().Foreground(lipgloss.Color("#5EEAD4"))
	m := &model{cfg: cfg, renderer: renderer, spin: s, bar: progress.New(progress.WithGradient("#14B8A6", "#818CF8"), progress.WithoutPercentage(), progress.WithColorProfile(renderer.ColorProfile())), width: 88, height: 35, ctx: ctx, cancel: cancel, work: work, events: make(chan sampleMsg, 128), completed: make(chan Result, 1)}
	names := []string{"CPU", "GPU"}
	if cfg.Mode == "record" {
		names = []string{"CPU", "GPU"}
	} else if cfg.Target != "both" {
		names = []string{strings.ToUpper(cfg.Target)}
	}
	for _, name := range names {
		m.cards = append(m.cards, card{name: name, state: "Waiting"})
	}
	return m
}
func refresh() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}
func (m *model) Init() tea.Cmd { return func() tea.Msg { return readyMsg{} } }
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case readyMsg:
		if m.started {
			return m, nil
		}
		m.started = true
		go func() {
			result := m.work(m.ctx, func(phase string, s thermal.Sample) {
				select {
				case m.events <- sampleMsg{phase, s}:
				default:
				}
			})
			m.completed <- result
		}()
		return m, tea.Batch(m.spin.Tick, refresh())
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" || msg.String() == "esc" {
			if !m.stopping {
				m.stopping = true
				m.cancel()
			}
		}
	case tickMsg:
		for {
			select {
			case event := <-m.events:
				m.sample(event)
			default:
				goto drained
			}
		}
	drained:
		select {
		case result := <-m.completed:
			m.finish(result)
			return m, tea.Quit
		default:
		}
		if m.ctx.Err() != nil {
			m.stopping = true
		}
		return m, refresh()
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}
func (m *model) sample(event sampleMsg) {
	m.phase = event.phase
	for i := range m.cards {
		c := &m.cards[i]
		if m.cfg.Mode != "record" && c.name != event.phase {
			if c.state == "Running" {
				c.state = "Awaiting result"
			}
			continue
		}
		c.state = "Running"
		c.seconds = event.sample.Seconds
		c.temp, c.power, c.clock, c.throttled = nil, nil, nil, nil
		var found *thermal.Device
		for j := range event.sample.Devices {
			d := &event.sample.Devices[j]
			if strings.EqualFold(d.Kind, c.name) && (found == nil || found.Temp == nil && d.Temp != nil) {
				found = d
			}
		}
		if found != nil {
			c.device = found.Name
			c.temp, c.power, c.clock, c.throttled = found.Temp, found.Power, found.Clock, found.Throttled
		}
		c.trace = append(c.trace, c.temp)
		if len(c.trace) > 60 {
			c.trace = c.trace[len(c.trace)-60:]
		}
	}
}
func number(n *float64, unit string) string {
	if n == nil {
		return "Unknown"
	}
	return fmt.Sprintf("%.1f%s", *n, unit)
}
func rate(n float64) string {
	for _, s := range []struct {
		n float64
		s string
	}{{1e12, "T"}, {1e9, "G"}, {1e6, "M"}, {1e3, "k"}} {
		if n >= s.n {
			return fmt.Sprintf("%.2f %s", n/s.n, s.s)
		}
	}
	return fmt.Sprintf("%.1f", n)
}
func (m *model) finish(result Result) {
	m.result = &result
	phases := result.Run.Phases
	if len(phases) == 0 {
		phases = []thermal.Run{result.Run}
	}
	for i := range m.cards {
		c := &m.cards[i]
		for _, phase := range phases {
			kind := "CPU"
			if phase.Workload == "gpu-integer-v1" {
				kind = "GPU"
			}
			if m.cfg.Mode != "record" && kind != c.name {
				continue
			}
			c.state = phase.Status
			if c.state == "" {
				c.state = "failed"
			}
			c.seconds = phase.Elapsed
			c.temp, c.power, c.clock, c.throttled = nil, nil, nil, nil
			c.trace = nil
			stats := thermal.Summarize(phase, true)
			var selected string
			for id, s := range stats {
				if !strings.EqualFold(s.Kind, c.name) {
					continue
				}
				if phase.GPU != nil && !strings.EqualFold(strings.TrimSpace(s.Name), strings.TrimSpace(phase.GPU.Device)) {
					continue
				}
				if selected == "" || stats[selected].Mean == nil && s.Mean != nil || stats[selected].Mean != nil && s.Mean != nil && *s.Mean > *stats[selected].Mean {
					selected = id
				}
			}
			s := stats[selected]
			c.device = s.Name
			c.temp, c.power, c.clock = s.Mean, s.Power, s.Clock
			if s.KnownThrottleSamples > 0 {
				v := s.ThrottleSamples > 0
				c.throttled = &v
			}
			for _, sample := range phase.Samples {
				var n *float64
				for _, d := range sample.Devices {
					if d.ID == selected {
						n = d.Temp
					}
				}
				c.trace = append(c.trace, n)
			}
			if len(c.trace) > 60 {
				c.trace = c.trace[len(c.trace)-60:]
			}
			if phase.GPU != nil {
				c.device = phase.GPU.Device
				if r := phase.GPU.Rate(); r != nil {
					c.score = rate(*r) + " iterations/s"
				}
			}
			if kind == "CPU" && phase.Operations > 0 && phase.Elapsed > 0 {
				c.score = rate(float64(phase.Operations)/phase.Elapsed) + " hashes/s"
			}
			if phase.Status != "complete" {
				c.notes = "See the saved report for this test's limitation."
				if len(phase.Warnings) > 0 {
					c.notes = phase.Warnings[len(phase.Warnings)-1]
				}
			}
		}
	}
}
func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
func (m *model) style(color string) lipgloss.Style {
	return m.renderer.NewStyle().Foreground(lipgloss.Color(color))
}
func (m *model) trace(values []*float64, width int) string {
	glyphs := []rune("▁▂▃▄▅▆▇█")
	if len(values) == 0 {
		return m.style("#64748B").Render("Waiting for temperature readings")
	}
	if len(values) > width {
		values = values[len(values)-width:]
	}
	var b strings.Builder
	for _, v := range values {
		if v == nil {
			b.WriteRune('·')
			continue
		}
		level := int(math.Round(math.Max(0, math.Min(1, (*v-30)/70)) * 7))
		b.WriteRune(glyphs[level])
	}
	return m.style("#5EEAD4").Render(b.String())
}
func (m *model) cardView(c card, width int) string {
	inner := max(12, width-6)
	stateColor := "#94A3B8"
	if c.state == "Running" || c.state == "complete" {
		stateColor = "#5EEAD4"
	} else if c.state != "Waiting" && c.state != "Awaiting result" {
		stateColor = "#FBBF24"
	}
	title := m.style("#E2E8F0").Bold(true).Render(c.name) + "  " + m.style(stateColor).Render(safe(c.state))
	device := c.device
	if device == "" {
		device = "Sensor discovery"
		if m.result != nil {
			device = "No target readings"
		}
	}
	if m.height < 30 && m.result == nil {
		throttle := "Throttle unknown"
		if c.throttled != nil {
			if *c.throttled {
				throttle = "Throttle observed"
			} else {
				throttle = "No throttle observed"
			}
		}
		rows := []string{title, m.style("#94A3B8").Render(ansi.Truncate(safe(device), inner, "…")),
			m.style("#5EEAD4").Bold(true).Render(number(c.temp, " °C")),
			m.trace(c.trace, inner),
			m.style("#CBD5E1").Render(number(c.power, " W") + " / " + number(c.clock, " MHz")),
			m.style("#94A3B8").Render(throttle)}
		return m.renderer.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#334155")).Padding(0, 2).Width(width - 2).Render(strings.Join(rows, "\n"))
	}
	lines := []string{title, m.style("#94A3B8").Render(ansi.Truncate(safe(device), inner, "…")), ""}
	if c.score != "" {
		lines = append(lines, m.style("#E2E8F0").Bold(true).Render(c.score))
	}
	label := "LIVE TEMPERATURE"
	if m.result != nil {
		label = "SUSTAINED TEMPERATURE"
	}
	lines = append(lines, m.style("#94A3B8").Render(label), m.style("#5EEAD4").Bold(true).Render(number(c.temp, " °C")))
	if m.height >= 27 || m.result != nil {
		lines = append(lines, "", m.trace(c.trace, inner), m.style("#64748B").Render("30–100 °C · gaps = unknown"))
	}
	lines = append(lines, "", m.style("#CBD5E1").Render(number(c.power, " W")+"  /  "+number(c.clock, " MHz")))
	throttle := "Throttle: unknown"
	if c.throttled != nil {
		if *c.throttled {
			throttle = "Throttle: observed"
		} else {
			throttle = "Throttle: not observed"
		}
	}
	lines = append(lines, m.style("#94A3B8").Render(throttle))
	if c.notes != "" {
		lines = append(lines, "", m.style("#FBBF24").Width(inner).Render(safe(c.notes)))
	}
	return m.renderer.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#334155")).Padding(1, 2).Width(width - 2).Render(strings.Join(lines, "\n"))
}
func (m *model) View() string {
	// Print final results after terminal restoration so tall reports remain in
	// scrollback instead of being clipped to the live renderer height.
	if m.result != nil {
		return ""
	}
	return m.render()
}
func (m *model) render() string {
	width := max(12, min(m.width-4, 104))
	title := m.style("#5EEAD4").Bold(true).Render("◈ THERMAL")
	subtitle := "Hardware insights, measured locally"
	state := m.spin.View() + " Preparing sensors"
	if m.phase != "" {
		state = m.spin.View() + " " + m.phase + " benchmark"
	}
	if m.cfg.Mode == "record" {
		state = m.spin.View() + " Recording telemetry"
	}
	if m.stopping {
		state = m.style("#FBBF24").Render("Stopping safely · saving partial results")
	}
	if m.result != nil {
		state = m.style("#5EEAD4").Render("Results saved")
		if m.result.Run.Status != "complete" {
			state = m.style("#FBBF24").Render("Run " + m.result.Run.Status)
		}
		if m.result.Err != nil {
			state = m.style("#FBBF24").Render("Report needs attention")
		}
	}
	header := title + "\n" + m.style("#94A3B8").Render(subtitle) + "\n\n" + state
	if m.height < 30 {
		header = title + "\n" + state
	}
	var panels []string
	compact := m.height < 22 || width < 68
	side := len(m.cards) == 2 && width >= 68 && !compact
	cardWidth := width
	if side {
		cardWidth = (width - 2) / 2
	}
	for _, c := range m.cards {
		if compact {
			line := m.style("#E2E8F0").Bold(true).Render(c.name) + "  " + m.style("#5EEAD4").Render(c.state)
			metric := number(c.temp, " °C") + " / " + number(c.power, " W")
			if c.score != "" {
				metric = c.score + " / " + number(c.temp, " °C")
			}
			if m.height < 16 {
				line += "  " + metric
			} else {
				line += "\n" + m.style("#94A3B8").Render(metric)
			}
			if c.notes != "" && m.result != nil {
				line += "\n" + m.style("#FBBF24").Width(width).Render(safe(c.notes))
			}
			panels = append(panels, line)
		} else {
			panels = append(panels, m.cardView(c, cardWidth))
		}
	}
	content := strings.Join(panels, "\n")
	if side {
		content = lipgloss.JoinHorizontal(lipgloss.Top, panels[0], "  ", panels[1])
	}
	footer := ""
	if m.result == nil {
		elapsed := 0.0
		for _, c := range m.cards {
			if c.state == "Running" {
				elapsed = c.seconds
			}
		}
		m.bar.Width = max(10, width-16)
		fraction := 0.0
		if m.cfg.Duration > 0 {
			fraction = math.Min(1, elapsed/m.cfg.Duration)
		}
		footer = m.bar.ViewAs(fraction) + fmt.Sprintf("  %3.0f%%", fraction*100) + "\n" +
			m.style("#94A3B8").Render(fmt.Sprintf("%.0f / %.0f seconds per test · stop limit %.0f °C", elapsed, m.cfg.Duration, m.cfg.StopTemp)) + "\n\n" +
			m.style("#64748B").Render("q / esc / ctrl+c  stop and save")
	} else {
		lines := []string{m.style("#94A3B8").Render("Final metrics use the last 25% of each test. Compute scores are not FPS.")}
		if m.result.Err != nil {
			lines = append(lines, m.style("#FBBF24").Width(width).Render(safe(m.result.Err.Error())))
		}
		for _, line := range strings.Split(strings.TrimSpace(m.result.Saved), "\n") {
			if line != "" {
				lines = append(lines, m.style("#CBD5E1").Width(width).Render(safe(line)))
			}
		}
		lines = append(lines, m.style("#64748B").Render("--no-tui  minimal output    --verbose  full tables"))
		footer = strings.Join(lines, "\n")
	}
	view := header + "\n\n" + content + "\n\n" + footer + "\n"
	// Narrow terminals must not wrap ANSI-styled lines outside the dashboard.
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, max(1, m.width-3), "")
	}
	if m.height < 16 && m.result == nil {
		short := title + "  " + state + "\n" + strings.Join(panels, "\n") + "\n" + m.style("#64748B").Render("q / ctrl+c · stop and save")
		return ansi.Hardwrap(short, max(1, m.width-1), false)
	}
	return m.renderer.NewStyle().Background(lipgloss.Color("#0B1120")).Padding(1).Render(strings.Join(lines, "\n"))
}

// Run never abandons an active capture: terminal errors cancel the workload and
// wait for its partial reports. A startup failure allows the caller to fall back.
func Run(ctx context.Context, input, output *os.File, cfg Config, work Work) (Result, bool, error) {
	restore, err := prepareTerminal(output)
	if err != nil {
		return Result{}, false, err
	}
	defer restore()
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := newModel(workCtx, cancel, cfg, output, work)
	program := tea.NewProgram(m, tea.WithInput(input), tea.WithOutput(output), tea.WithoutSignalHandler(), tea.WithFPS(15))
	_, err = program.Run()
	if err != nil {
		_ = program.ReleaseTerminal()
	}
	if !m.started {
		return Result{}, false, err
	}
	if m.result != nil {
		if err == nil {
			fmt.Fprint(output, m.render())
		}
		return *m.result, true, err
	}
	cancel()
	result := <-m.completed
	return result, true, err
}
