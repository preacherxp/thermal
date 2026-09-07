package thermal

import (
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"unicode"
)

type display struct {
	w     io.Writer
	color bool
}

func newDisplay(w io.Writer) display {
	color := false
	if f, ok := w.(*os.File); ok {
		color = TerminalFile(f)
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		color = false
	}
	if os.Getenv("TERM") == "dumb" {
		color = false
	}
	return display{w, color}
}
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
func clip(s string, n int) string {
	r := []rune(clean(s))
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return string(r)
}
func (d display) paint(code, s string) string {
	if !d.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}
func (d display) header(title, subtitle string) {
	fmt.Fprintln(d.w, d.paint("36", "╭─ THERMAL  "+strings.Repeat("─", 61)))
	fmt.Fprintln(d.w, "│  "+d.paint("1", clean(title)))
	if subtitle != "" {
		fmt.Fprintln(d.w, "│  "+clean(subtitle))
	}
	fmt.Fprintln(d.w, d.paint("36", "╰"+strings.Repeat("─", 73)))
}
func (d display) section(title string) { fmt.Fprintln(d.w, "\n  "+d.paint("1;36", clean(title))) }
func (d display) line(s string)        { fmt.Fprintln(d.w, "  "+clean(s)) }
func (d display) bullet(s string) {
	words := strings.Fields(clean(s))
	line := "  • "
	for _, word := range words {
		if len([]rune(line))+len([]rune(word))+1 > 76 {
			fmt.Fprintln(d.w, line)
			line = "    "
		}
		if line != "  • " && line != "    " {
			line += " "
		}
		line += word
	}
	fmt.Fprintln(d.w, line)
}
func (d display) warnings(xs []string) {
	if len(xs) == 0 {
		return
	}
	d.section("NOTES")
	for _, s := range xs {
		d.bullet(s)
	}
}
func signed(v *float64, unit string) string {
	if v == nil {
		return "unknown"
	}
	return fmt.Sprintf("%+.1f%s", *v, unit)
}
func (d display) metric(label string, before, after, change string) {
	fmt.Fprintf(d.w, "  %-32s %11s %11s %13s\n", clip(label, 32), before, after, change)
}
func (d display) powerTable(stats []PowerStats) {
	for _, limits := range []bool{false, true} {
		visible := []PowerStats{}
		for _, s := range stats {
			if (s.Mode == "limit") == limits {
				visible = append(visible, s)
			}
		}
		if len(visible) == 0 {
			continue
		}
		title := "POWER DRAW (W)"
		if limits {
			title = "POWER LIMITS (W) · not consumption"
		}
		d.line(title)
		fmt.Fprintf(d.w, "  %-32s %9s %9s %9s %9s\n", "CHANNEL", "LAST", "MIN", "MEAN", "PEAK")
		for _, s := range visible {
			fmt.Fprintf(d.w, "  %-32s %9s %9s %9s %9s\n", clip(s.Name, 32), value(s.Current, ""), value(s.Min, ""), value(s.Mean, ""), value(s.Peak, ""))
		}
	}
}
func powerList(m map[string]PowerStats) []PowerStats {
	ids := []string{}
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := []PowerStats{}
	for _, id := range ids {
		out = append(out, m[id])
	}
	return out
}
func Report(w io.Writer, r Run) {
	d := newDisplay(w)
	d.header(strings.ToUpper(r.Stage)+"  /  "+strings.ToUpper(r.Status), r.Workload+"  ·  "+r.Created.Format("2006-01-02 15:04 UTC"))
	if len(r.Phases) > 0 {
		d.line("Sequential CPU and GPU benchmarks; each phase has its own measurements.")
		for _, phase := range r.Phases {
			fmt.Fprintln(w)
			Report(w, phase)
		}
		d.warnings(r.Warnings)
		return
	}
	d.line(fmt.Sprintf("Profile %s  ·  Power source %s  ·  %d samples", r.Profile, r.PowerSource, len(r.Samples)))
	if r.Preparation != nil {
		d.section("PRE-RUN CONTEXT")
		d.line(r.Preparation.Hardware.CPU + " · " + r.Preparation.Hardware.Family)
		d.line("CPU material: " + r.Preparation.CPUMaterial.Current)
		for _, g := range r.Preparation.GPUs {
			d.line(g.Name + " · rated " + value(g.RatedWatts, " W") + " · material " + g.Material.Current)
		}
	}
	stats := Summarize(r, false)
	powers := SummarizePower(r, false)
	for _, id := range keys(stats) {
		s := stats[id]
		d.section(strings.ToUpper(s.Kind) + "  /  " + s.Name)
		d.line("Temperature  " + value(s.Mean, " C") + " mean  /  " + value(s.Peak, " C") + " peak")
		if s.Util != nil || s.Clock != nil {
			d.line("Load  " + value(s.Util, "%") + "  ·  Clock  " + value(s.Clock, " MHz"))
		}
		if s.KnownThrottleSamples > 0 {
			d.line("Thermal throttling  " + throttle(s))
		}
		d.powerTable(powerList(powers[id]))
		if len(powers[id]) == 0 {
			d.line("Power draw  unknown")
		}
	}
	if len(stats) == 0 {
		d.line("No hardware readings available. Run thermal doctor for setup.")
	}
	if r.Operations > 0 && r.Elapsed > 0 {
		d.section("CPU BENCHMARK")
		d.line(fmt.Sprintf("%.0f hashes/s  ·  %d workers  ·  %.1fs", float64(r.Operations)/r.Elapsed, r.Workers, r.Elapsed))
	}
	if r.GPU != nil {
		d.section("GPU COMPUTE BENCHMARK")
		d.line(r.GPU.Device + " · " + r.GPU.Backend)
		d.line(value(r.GPU.Rate(), " verified iterations/s") + fmt.Sprintf(" · %.1fs", r.GPU.Elapsed))
		d.line("Integer compute workload; this is not a graphics FPS or third-party benchmark score.")
	}
	if len(r.Apps) > 0 {
		d.section("BUSY APPS BEFORE CAPTURE")
		for _, a := range r.Apps {
			fmt.Fprintf(w, "  %-32s %7.1f%%  PID %d\n", clip(a.Name, 32), a.CPU, a.PID)
		}
		d.line("100% = one logical CPU; brief pre-test sample.")
	}
	d.section("NEXT STEPS")
	for _, s := range Recommend(r) {
		d.bullet(s)
	}
	if len(powers) > 0 {
		d.line("Power channels may overlap; they are not added together.")
	}
	d.warnings(r.Warnings)
}
func PrintSnapshot(w io.Writer, s Sample, warnings []string, platform string) {
	d := newDisplay(w)
	d.header("SENSOR CHECK", platform+"  ·  live hardware readings")
	r := NewRun()
	r.Samples = []Sample{s}
	powers := SummarizePower(r, false)
	for _, device := range s.Devices {
		d.section(strings.ToUpper(device.Kind) + "  /  " + device.Name)
		d.line(device.Source)
		if device.Temp != nil || device.Util != nil || device.Clock != nil {
			d.line("Temperature " + value(device.Temp, " C") + "  ·  Load " + value(device.Util, "%") + "  ·  Clock " + value(device.Clock, " MHz"))
		}
		if device.Throttled != nil {
			state := "not active"
			if *device.Throttled {
				state = "ACTIVE"
			}
			d.line("Thermal throttling " + state)
		}
		channels := powerList(powers[device.ID])
		if len(channels) == 0 {
			d.line("Power draw  unknown")
		} else {
			for _, limit := range []bool{false, true} {
				title := "POWER DRAW"
				if limit {
					title = "POWER LIMITS · not consumption"
				}
				printed := false
				for _, p := range channels {
					if (p.Mode == "limit") != limit {
						continue
					}
					if !printed {
						d.line(title)
						printed = true
					}
					fmt.Fprintf(w, "  %-47s %12s\n", clip(p.Name, 47), value(p.Current, " W"))
				}
			}
		}
	}
	if len(s.Devices) == 0 {
		d.line("No hardware sensors found.")
	}
	d.warnings(warnings)
	d.section("SENSOR SETUP")
	d.bullet("Windows: direct NVIDIA driver telemetry; CPU temperature/power may be unavailable.")
	d.bullet("Linux: hwmon power channels and readable RAPL energy counters. RAPL watts are interval averages; permissions may restrict access.")
	d.bullet("macOS: native thermal telemetry is unavailable; readings stay unknown.")
	d.bullet("Optional LHM, macmon, and nvidia-smi integrations: set THERMAL_EXTERNAL_PROVIDERS=1.")
	d.line("Channels overlap. Board draw, CPU domains and limits are not totals to sum.")
}
func PrintComparison(w io.Writer, a, b Run, c Comparison) {
	d := newDisplay(w)
	d.header("BEFORE → AFTER", a.Stage+" → "+b.Stage+"  ·  last 25% of recording")
	if len(c.Phases) > 0 {
		for _, compared := range c.Phases {
			var before, after Run
			for _, phase := range a.Phases {
				if phase.Workload == compared.Workload {
					before = phase
				}
			}
			for _, phase := range b.Phases {
				if phase.Workload == compared.Workload {
					after = phase
				}
			}
			PrintComparison(w, before, after, compared.Comparison)
		}
		d.warnings(c.Warnings)
		return
	}
	for _, device := range c.Devices {
		d.section(strings.ToUpper(device.Before.Kind) + "  /  " + device.Name)
		d.metric("METRIC", "BEFORE", "AFTER", "CHANGE")
		d.metric("Temperature (mean)", value(device.Before.Mean, " C"), value(device.After.Mean, " C"), signed(device.Temperature, " C"))
		d.metric("Clock (mean)", value(device.Before.Clock, " MHz"), value(device.After.Clock, " MHz"), signed(device.Clock, " MHz"))
		if len(device.PowerChannels) == 0 {
			d.metric("Power (mean)", value(device.Before.Power, " W"), value(device.After.Power, " W"), signed(device.Power, " W"))
		}
		for _, p := range device.PowerChannels {
			label := p.Name + " mean"
			if p.Mode == "limit" {
				label = p.Name + " [limit]"
			}
			d.metric(label, value(p.Before.Mean, " W"), value(p.After.Mean, " W"), signed(p.MeanDelta, " W"))
			if p.Mode != "limit" {
				d.metric("  peak", value(p.Before.Peak, " W"), value(p.After.Peak, " W"), signed(p.PeakDelta, " W"))
			}
		}
		d.line("Throttling  " + throttle(device.Before) + " → " + throttle(device.After))
	}
	if c.ThroughputDelta != nil {
		d.section("CPU THROUGHPUT")
		d.metric("Hashes / second", value(c.BeforeThroughput, ""), value(c.AfterThroughput, ""), signed(c.ThroughputDelta, ""))
	}
	if c.GPUThroughputDelta != nil {
		d.section("GPU COMPUTE THROUGHPUT")
		d.metric("Verified iterations / second", value(a.GPU.Rate(), ""), value(b.GPU.Rate(), ""), signed(c.GPUThroughputDelta, ""))
	}
	d.section("READING THE RESULT")
	d.bullet("Similar temperatures with higher sustained clocks/power can indicate improved cooling at comparable load. These measurements do not prove the cause.")
	d.warnings(c.Warnings)
	d.section("NEXT STEPS")
	for _, s := range Recommend(b) {
		d.bullet(s)
	}
}
func NewProgress(w io.Writer, duration float64) (func(Sample), func()) {
	d := newDisplay(w)
	update := func(s Sample) {
		fraction := math.Min(1, math.Max(0, s.Seconds/duration))
		filled := int(fraction * 12)
		line := fmt.Sprintf("%5.1fs [%s%s] %3.0f%%", s.Seconds, strings.Repeat("=", filled), strings.Repeat(" ", 12-filled), fraction*100)
		for _, kind := range []string{"cpu", "gpu"} {
			var chosen *Device
			for i := range s.Devices {
				v := &s.Devices[i]
				if v.Kind == kind && (chosen == nil || chosen.Power == nil && v.Power != nil) {
					chosen = v
				}
			}
			if chosen != nil {
				line += "  " + strings.ToUpper(kind) + " " + value(chosen.Temp, "C") + " / " + value(chosen.Power, "W")
			}
		}
		if d.color {
			fmt.Fprint(w, "\r\x1b[2K"+clip(line, 100))
		} else {
			fmt.Fprintln(w, "  "+line)
		}
	}
	finish := func() {
		if d.color {
			fmt.Fprintln(w)
		}
	}
	return update, finish
}
