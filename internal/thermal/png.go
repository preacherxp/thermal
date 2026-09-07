package thermal

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// The pre-rendered Go font keeps PNG export independent of system fonts/cgo.
//
//go:embed assets/font.png assets/font.json
var reportAssets embed.FS

type glyph struct{ X, Y, W, H, Advance int }

var fontOnce sync.Once
var fontImage image.Image
var fontGlyphs map[string]glyph
var fontError error

func loadReportFont() error {
	fontOnce.Do(func() {
		b, _ := reportAssets.ReadFile("assets/font.png")
		fontImage, fontError = png.Decode(bytes.NewReader(b))
		if fontError != nil {
			return
		}
		b, _ = reportAssets.ReadFile("assets/font.json")
		fontError = json.Unmarshal(bytes.TrimPrefix(b, []byte{239, 187, 191}), &fontGlyphs)
	})
	return fontError
}

var (
	ink    = color.RGBA{25, 42, 62, 255}
	muted  = color.RGBA{91, 109, 129, 255}
	paper  = color.RGBA{243, 246, 250, 255}
	white  = color.RGBA{255, 255, 255, 255}
	teal   = color.RGBA{0, 126, 132, 255}
	orange = color.RGBA{194, 99, 35, 255}
	grid   = color.RGBA{219, 227, 235, 255}
)

type pngPanel struct {
	height int
	paint  func(*image.RGBA, int)
}
type pngReport struct{ panels []pngPanel }

func rect(im *image.RGBA, x, y, w, h int, c color.RGBA) {
	draw.Draw(im, image.Rect(x, y, x+w, y+h), image.NewUniform(c), image.Point{}, draw.Src)
}
func fontGlyph(size int, r rune) glyph {
	g, ok := fontGlyphs[fmt.Sprintf("%d:%d", size, r)]
	if !ok {
		g = fontGlyphs[fmt.Sprintf("%d:63", size)]
	}
	return g
}
func textWidth(s string, size int) int {
	w := 0
	for _, r := range s {
		w += fontGlyph(size, r).Advance
	}
	return w
}
func pngText(im *image.RGBA, x, y, size int, c color.RGBA, s string) {
	for _, r := range s {
		g := fontGlyph(size, r)
		draw.DrawMask(im, image.Rect(x-2, y-2, x-2+g.W, y-2+g.H), image.NewUniform(c), image.Point{}, fontImage, image.Pt(g.X, g.Y), draw.Over)
		x += g.Advance
	}
}
func wrapPNG(s string, size, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line != "" && textWidth(line+" "+word, size) <= width {
			line += " " + word
			continue
		}
		if line != "" {
			lines = append(lines, line)
			line = ""
		}
		for _, r := range word {
			if textWidth(line+string(r), size) > width && line != "" {
				lines = append(lines, line)
				line = ""
			}
			line += string(r)
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
func (p *pngReport) paragraph(title string, lines []string, accent color.RGBA) {
	var wrapped []string
	for _, s := range lines {
		wrapped = append(wrapped, wrapPNG(s, 24, 1240)...)
		wrapped = append(wrapped, "")
	}
	if len(wrapped) > 0 {
		wrapped = wrapped[:len(wrapped)-1]
	}
	p.panels = append(p.panels, pngPanel{90 + len(wrapped)*32, func(im *image.RGBA, y int) {
		rect(im, 48, y, 1344, 90+len(wrapped)*32, white)
		rect(im, 48, y, 5, 90+len(wrapped)*32, accent)
		pngText(im, 80, y+22, 24, accent, title)
		for i, s := range wrapped {
			pngText(im, 80, y+65+i*32, 24, ink, s)
		}
	}})
}
func runContext(r Run) string {
	return fmt.Sprintf("%s  |  %s  |  %s  |  %.0fs recorded  |  %s", r.Stage, r.Profile, r.PowerSource, runSeconds(r), r.Status)
}
func runSeconds(r Run) float64 {
	if len(r.Phases) > 0 {
		return r.Elapsed
	}
	if len(r.Samples) > 0 {
		return r.Samples[len(r.Samples)-1].Seconds
	}
	return 0
}
func (p *pngReport) header(title string, a Run, b *Run) {
	lines := []string{a.Host + "  |  " + a.Workload + "  |  " + a.Created.Format("2006-01-02 15:04 MST"), runContext(a)}
	if b != nil {
		lines = append(lines, "After: "+b.Host+"  |  "+b.Workload+"  |  "+b.Created.Format("2006-01-02 15:04 MST"), runContext(*b))
	}
	var wrapped []string
	for _, s := range lines {
		wrapped = append(wrapped, wrapPNG(s, 24, 1240)...)
	}
	height := 148 + len(wrapped)*34
	p.panels = append(p.panels, pngPanel{height, func(im *image.RGBA, y int) {
		rect(im, 48, y, 1344, height, ink)
		pngText(im, 80, y+22, 18, color.RGBA{103, 222, 213, 255}, "THERMAL / MEASURE. CHANGE ONE THING. COMPARE.")
		pngText(im, 80, y+55, 52, white, title)
		for i, s := range wrapped {
			pngText(im, 80, y+126+i*34, 24, white, s)
		}
	}})
}
func pngValue(v *float64, unit string) string {
	if v == nil {
		return "Unknown"
	}
	if math.Abs(*v) >= 1e6 {
		return fmt.Sprintf("%.3g%s", *v, unit)
	}
	return fmt.Sprintf("%.1f%s", *v, unit)
}
func pngDelta(v *float64, unit string) string {
	if v == nil {
		return "Change unknown"
	}
	return fmt.Sprintf("%+.1f%s", *v, unit)
}
func pngLine(im *image.RGBA, x0, y0, x1, y1 int, c color.RGBA, thickness int) {
	dx, dy := x1-x0, y1-y0
	steps := max(absPNG(dx), absPNG(dy))
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(max(1, steps))
		x, y := x0+int(float64(dx)*t), y0+int(float64(dy)*t)
		rect(im, x-thickness/2, y-thickness/2, thickness, thickness, c)
	}
}
func absPNG(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Chart gaps remain gaps when a sensor has no reading; unknown is never zero.
func temperatureChart(im *image.RGBA, y int, id string, a Run, b *Run) {
	runs := []Run{a}
	colors := []color.RGBA{teal}
	if b != nil {
		runs = append(runs, *b)
		colors = []color.RGBA{orange, teal}
	}
	lo, hi, end := math.Inf(1), math.Inf(-1), 0.0
	for _, r := range runs {
		end = math.Max(end, runSeconds(r))
		for _, s := range r.Samples {
			for _, d := range s.Devices {
				if d.ID == id && d.Temp != nil {
					lo = math.Min(lo, *d.Temp)
					hi = math.Max(hi, *d.Temp)
				}
			}
		}
	}
	if math.IsInf(lo, 1) {
		pngText(im, 100, y+80, 24, muted, "Temperature unavailable. Run thermal doctor to check sensor access.")
		return
	}
	lo = math.Floor((lo-5)/10) * 10
	hi = math.Ceil((hi+5)/10) * 10
	const left, right, height = 154, 1320, 190
	for i := 0; i <= 4; i++ {
		yy := y + i*height/4
		rect(im, left, yy, right-left, 1, grid)
		pngText(im, 80, yy-12, 18, muted, fmt.Sprintf("%.0f C", hi-float64(i)*(hi-lo)/4))
	}
	for i := 0; i <= 4; i++ {
		xx := left + i*(right-left)/4
		pngText(im, xx-15, y+height+12, 18, muted, fmt.Sprintf("%.0fs", end*float64(i)/4))
	}
	for j, r := range runs {
		thickness := 3
		if b != nil && j == 0 {
			thickness = 7
		}
		prev := false
		px, py := 0, 0
		for _, s := range r.Samples {
			var temp *float64
			for _, d := range s.Devices {
				if d.ID == id {
					temp = d.Temp
					break
				}
			}
			if temp == nil {
				prev = false
				continue
			}
			xx := left + int(s.Seconds/math.Max(end, 1)*(right-left))
			yy := y + int((hi-*temp)/(hi-lo)*height)
			if prev {
				pngLine(im, px, py, xx, yy, colors[j], thickness)
			}
			rect(im, xx-3, yy-3, 6, 6, colors[j])
			px, py, prev = xx, yy, true
		}
	}
}
func (p *pngReport) device(id string, a Run, b *Run, before, after Stats) {
	name := before.Name
	if name == "" {
		name = after.Name
	}
	title := wrapPNG(name, 36, 1240)
	bh := len(title) * 44
	p.panels = append(p.panels, pngPanel{bh + 520, func(im *image.RGBA, y int) {
		rect(im, 48, y, 1344, bh+520, white)
		for i, s := range title {
			pngText(im, 80, y+20+i*44, 36, ink, s)
		}
		y += bh
		legend := "Temperature over time"
		if b != nil {
			legend = "Temperature over time  |  Orange: before  /  Teal: after"
		}
		pngText(im, 80, y+26, 18, muted, legend)
		temperatureChart(im, y+76, id, a, b)
		labels := []string{"Sustained temperature", "Peak temperature", "Sustained power", "Sustained clock"}
		fullA := Summarize(a, false)[id]
		fullB := Stats{}
		if b != nil {
			fullB = Summarize(*b, false)[id]
		}
		av := []*float64{before.Mean, fullA.Peak, before.Power, before.Clock}
		bv := []*float64{after.Mean, fullB.Peak, after.Power, after.Clock}
		units := []string{" C", " C", " W", " MHz"}
		for i, label := range labels {
			x := 80 + i*324
			rect(im, x, y+324, 308, 144, paper)
			pngText(im, x+14, y+335, 18, muted, label)
			if b == nil {
				pngText(im, x+14, y+373, 36, ink, pngValue(av[i], units[i]))
			} else {
				pngText(im, x+14, y+369, 18, ink, pngValue(av[i], units[i])+" → "+pngValue(bv[i], units[i]))
				pngText(im, x+14, y+414, 24, teal, pngDelta(delta(av[i], bv[i]), units[i]))
			}
		}
		line := "Throttling: " + throttle(before) + "  |  Sustained load: " + pngValue(before.Util, "%")
		if b != nil {
			line = "Throttling: " + throttle(before) + " → " + throttle(after) + "  |  Load: " + pngValue(before.Util, "%") + " → " + pngValue(after.Util, "%")
		}
		pngText(im, 80, y+482, 18, muted, line)
	}})
}

// ponytail: six charts per phase keep suites readable; use JSON or thermal
// report for every sensor, and add paginated images if full charts are needed.
func (p *pngReport) devices(a Run, b *Run, devices []DeviceDelta) {
	selected := a
	if b != nil {
		selected = *b
	}
	cpu, _ := TargetStats(selected, "cpu")
	gpu, _ := TargetStats(selected, "gpu")
	slices.SortStableFunc(devices, func(a, b DeviceDelta) int {
		priority := func(id string) int {
			if id == cpu || id == gpu {
				return 0
			}
			return 1
		}
		return priority(a.ID) - priority(b.ID)
	})
	const limit = 6
	for _, d := range devices[:min(len(devices), limit)] {
		p.device(d.ID, a, b, d.Before, d.After)
	}
	if len(devices) > limit {
		p.paragraph("CHART OVERVIEW", []string{fmt.Sprintf("Showing %d of %d sensors; %d additional sensor charts omitted. All readings remain in the saved JSON and thermal report <run.json>.", limit, len(devices), len(devices)-limit)}, orange)
	}
}

func (p *pngReport) addReport(before Run, after *Run) {
	if len(before.Phases) > 0 {
		p.header("CPU + GPU benchmark", before, after)
		p.paragraph("BENCHMARK RESULTS", []string{"CPU and GPU are tested sequentially. Scores and thermal readings below belong to each individual test."}, teal)
		for _, phase := range before.Phases {
			var next *Run
			if after != nil {
				for _, candidate := range after.Phases {
					if candidate.Workload == phase.Workload {
						copy := candidate
						next = &copy
						break
					}
				}
			}
			p.addReport(phase, next)
		}
		if len(before.Warnings) > 0 {
			p.paragraph("SUITE NOTES", before.Warnings, orange)
		}
		if after != nil && len(after.Warnings) > 0 {
			p.paragraph("AFTER SUITE NOTES", after.Warnings, orange)
		}
		return
	}
	title := "Your thermal report"
	if before.Workload == "cpu-sha256-v1" {
		title = "CPU benchmark"
	}
	if before.Workload == "gpu-integer-v1" {
		title = "GPU benchmark"
	}
	current := before
	if after != nil {
		title = "Before & after"
		current = *after
	}
	p.header(title, before, after)
	for i, r := range []Run{before, current} {
		if i == 1 && after == nil {
			continue
		}
		label := ""
		if after != nil {
			label = "BEFORE / "
			if i == 1 {
				label = "AFTER / "
			}
		}
		if r.Workload == "cpu-sha256-v1" {
			var rate *float64
			if r.Operations > 0 && r.Elapsed > 0 {
				rate = Number(float64(r.Operations) / r.Elapsed)
			}
			p.paragraph(label+"CPU SCORE", []string{pngValue(rate, " hashes/s") + fmt.Sprintf(" | %d workers | %.1fs | %s", r.Workers, r.Elapsed, r.Status)}, teal)
		}
		if r.Workload == "gpu-integer-v1" {
			lines := []string{"Score unavailable | " + r.Status}
			if r.GPU != nil {
				lines = []string{r.GPU.Device + " | " + r.GPU.Backend, pngValue(r.GPU.Rate(), " verified iterations/s") + fmt.Sprintf(" | %.1fs | %s", r.GPU.Elapsed, r.Status)}
			}
			lines = append(lines, "Integer compute workload. Not graphics FPS or a third-party benchmark score.")
			p.paragraph(label+"GPU SCORE", lines, teal)
		}
	}
	p.paragraph("HOW TO READ THIS", []string{"Sustained values use the final 25% of recorded time. Peak temperature uses the whole run. Unknown means the sensor did not provide a reading. Throttling counts refer to the sustained window."}, teal)
	sa := Summarize(before, true)
	if after == nil {
		all := Summarize(before, false)
		var devices []DeviceDelta
		for _, id := range keys(all) {
			s := sa[id]
			s.Name = all[id].Name
			devices = append(devices, DeviceDelta{ID: id, Before: s})
		}
		p.devices(before, nil, devices)
		if len(all) == 0 {
			p.paragraph("NO SENSOR DATA", []string{"No device readings were recorded. Run thermal doctor to check sensor access."}, orange)
		}
	} else {
		c := Compare(before, *after)
		if c.GPUThroughputDelta != nil {
			p.paragraph("GPU WORK COMPLETED", []string{pngValue(before.GPU.Rate(), " iterations/s") + " → " + pngValue(after.GPU.Rate(), " iterations/s") + " (" + pngDelta(c.GPUThroughputDelta, " iterations/s") + ")"}, teal)
		}
		p.devices(before, after, c.Devices)
		if c.ThroughputDelta != nil {
			p.paragraph("CPU WORK COMPLETED", []string{pngValue(c.BeforeThroughput, " hashes/s") + " → " + pngValue(c.AfterThroughput, " hashes/s") + "  (" + pngDelta(c.ThroughputDelta, " hashes/s") + ")"}, teal)
		}
		p.paragraph("WHAT CHANGED", []string{"Similar temperatures with higher sustained clocks or power can indicate improved cooling at comparable load. These measurements do not prove the cause. Higher power alone is not a cooling score."}, teal)
		if len(c.Warnings) > 0 {
			p.paragraph("COMPARISON CAUTIONS", c.Warnings, orange)
		}
	}
	if before.Status != "complete" {
		p.paragraph("RUN STATUS", []string{"Before / run: " + before.Status + ". Measurements may cover only part of the requested run."}, orange)
	}
	if after != nil && after.Status != "complete" {
		p.paragraph("RUN STATUS", []string{"After: " + after.Status + ". Measurements may cover only part of the requested run."}, orange)
	}
	if len(before.Warnings) > 0 {
		p.paragraph("RUN NOTES / WARNINGS", before.Warnings, orange)
	}
	if after != nil && len(after.Warnings) > 0 {
		p.paragraph("AFTER RUN / WARNINGS", after.Warnings, orange)
	}
	if current.Notes != "" {
		p.paragraph("YOUR NOTES", []string{current.Notes}, teal)
	}
	p.paragraph("NEXT STEPS", Recommend(current), teal)
}

func WriteReportPNG(w io.Writer, before Run, after *Run) error {
	if err := loadReportFont(); err != nil {
		return err
	}
	p := pngReport{}
	p.addReport(before, after)
	height := 64
	for _, panel := range p.panels {
		height += panel.height + 20
		if height > 24000 {
			return fmt.Errorf("PNG report is too tall; use the text or JSON report for this run")
		}
	}
	im := image.NewRGBA(image.Rect(0, 0, 1440, height))
	rect(im, 0, 0, 1440, height, paper)
	y := 32
	for _, panel := range p.panels {
		panel.paint(im, y)
		y += panel.height + 20
	}
	return png.Encode(w, im)
}

func SaveReportPNG(path string, before Run, after *Run) error {
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		return fmt.Errorf("PNG output must end in .png")
	}
	var data bytes.Buffer
	if err := WriteReportPNG(&data, before, after); err != nil {
		return err
	}
	return saveExclusive(path, data.Bytes())
}
