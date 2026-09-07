package thermal

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"
)

type pdfFinding struct {
	title, body string
	caution     bool
}
type pdfReport struct {
	doc  pdfDocument
	page *pdfPage
	y    float64
	err  error
}

func pdfPhaseName(r Run) string {
	switch r.Workload {
	case "cpu-sha256-v1":
		return "CPU benchmark"
	case "gpu-integer-v1":
		return "GPU benchmark"
	case "cpu-gpu-suite-v1":
		return "CPU + GPU benchmarks"
	}
	return "Thermal recording"
}
func pdfRuns(r Run) []Run {
	if len(r.Phases) > 0 {
		return r.Phases
	}
	return []Run{r}
}
func pdfScore(r Run) (string, string) {
	if r.Workload == "cpu-sha256-v1" && r.Operations > 0 && r.Elapsed > 0 {
		return pdfNumber(Number(float64(r.Operations)/r.Elapsed), "", true), "SHA-256 hashes / second"
	}
	if r.GPU != nil && r.GPU.Rate() != nil {
		return pdfNumber(r.GPU.Rate(), "", true), "verified integer iterations / second"
	}
	if r.Status == "refused" || r.Status == "skipped" || r.Status == "unavailable" {
		return "Not run", r.Status
	}
	return "Unknown", "score unavailable"
}
func pdfNumber(n *float64, unit string, compact bool) string {
	if n == nil {
		return "Unknown"
	}
	v := *n
	if compact {
		return CompactRate(v) + unit
	}
	return fmt.Sprintf("%.1f%s", v, unit)
}
func pdfFindings(r Run) []pdfFinding {
	var findings []pdfFinding
	for _, phase := range pdfRuns(r) {
		name := pdfPhaseName(phase)
		id, s := TargetStats(phase, "")
		if phase.Status != "complete" {
			body := "Status: " + phase.Status + ". "
			switch phase.Status {
			case "refused":
				body += "The test did not start because its temperature or monitoring guard was not satisfied. Check the notes before repeating."
			case "unavailable":
				body += "The required device or benchmark backend was unavailable."
			case "skipped":
				body += "This test was skipped after an earlier stop or cancellation."
			default:
				body += "Only partial measurements are available; its score is not a full-duration result."
			}
			findings = append(findings, pdfFinding{name + " incomplete", body, true})
		}
		stats := map[string]Stats{id: s}
		recording := phase.Workload != "cpu-sha256-v1" && phase.Workload != "gpu-integer-v1"
		if recording {
			stats = Summarize(phase, true)
			for id, s := range stats {
				if s.Kind != "cpu" && s.Kind != "gpu" {
					delete(stats, id)
				}
			}
			if len(stats) == 0 {
				stats[""] = Stats{}
			}
		}
		full := Summarize(phase, false)
		for _, id := range keys(stats) {
			s := stats[id]
			name := name
			if recording && s.Name != "" {
				name += " / " + s.Name
			}
			if s.Mean == nil {
				if phase.Status != "complete" {
					continue
				}
				findings = append(findings, pdfFinding{name + ": temperature unknown", "No usable target temperature was measured. A throughput score alone cannot establish thermal condition.", true})
				continue
			}
			peak := full[id].Peak
			detail := fmt.Sprintf("%s sustained; %s peak.", pdfNumber(s.Mean, " °C", false), pdfNumber(peak, " °C", false))
			threshold := 85.0
			if s.Kind == "gpu" {
				threshold = 80
			}
			title := "No elevated sustained temperature observed"
			caution := *s.Mean >= threshold || s.ThrottleSamples > 0
			if caution {
				title = "Elevated thermal readings"
			}
			if s.KnownThrottleSamples > 0 {
				detail += fmt.Sprintf(" Thermal throttling: %d of %d known samples.", s.ThrottleSamples, s.KnownThrottleSamples)
			} else {
				detail += " Throttling status is unknown."
			}
			if s.Count < 3 {
				title = "Limited thermal evidence"
				detail += " Fewer than three temperature samples cover the sustained window."
				caution = true
			}
			findings = append(findings, pdfFinding{name + ": " + title, detail, caution})
		}
	}
	return findings
}
func (p *pdfReport) newPage(label string) {
	if len(p.doc.pages) >= 200 {
		p.err = fmt.Errorf("PDF report exceeds 200 pages")
		return
	}
	p.page = p.doc.page()
	p.y = 92
	p.page.text(pdfMargin, 39, 10, pdfAccent, "THERMAL")
	p.page.text(pdfMargin+76, 41, 8, pdfMuted, strings.ToUpper(label))
	p.page.line(pdfMargin, 66, pdfWidth-pdfMargin, 66, 0.6, pdfRule)
}
func (p *pdfReport) ensure(h float64) {
	if p.err != nil {
		return
	}
	if p.y+h > pdfHeight-70 {
		p.newPage("Findings / continued")
	}
}
func (p *pdfReport) heading(s string) {
	p.ensure(50)
	p.page.text(pdfMargin, p.y, 14, pdfInk, s)
	p.y += 30
}
func (p *pdfReport) paragraph(s string, color pdfColor) {
	for _, line := range p.doc.font.wrap(s, 10, pdfContentWidth) {
		p.ensure(15)
		if p.err != nil {
			return
		}
		p.page.text(pdfMargin, p.y, 10, color, line)
		p.y += 15
	}
	p.y += 9
}
func (p *pdfReport) finding(f pdfFinding) {
	title := p.doc.font.wrap(f.title, 11, pdfContentWidth-15)
	body := p.doc.font.wrap(f.body, 10, pdfContentWidth-15)
	p.ensure(float64(len(title)+len(body))*15 + 23)
	c := pdfAccent
	if f.caution {
		c = pdfAmber
	}
	p.page.rect(pdfMargin, p.y+4, 3, 10, c)
	for _, line := range title {
		p.ensure(16)
		p.page.text(pdfMargin+14, p.y, 11, pdfInk, line)
		p.y += 16
	}
	p.y += 3
	for _, line := range body {
		p.ensure(15)
		if p.err != nil {
			return
		}
		p.page.text(pdfMargin+14, p.y, 10, pdfMuted, line)
		p.y += 15
	}
	p.y += 17
}
func (p *pdfReport) status(r Run) {
	c := pdfAccent
	if r.Status != "complete" {
		c = pdfAmber
	}
	s := strings.ToUpper(r.Status)
	w := p.doc.font.width(s, 9) + 24
	p.page.rect(pdfMargin, p.y, w, 25, pdfPaper)
	p.page.text(pdfMargin+12, p.y+7, 9, c, s)
	p.y += 44
}
func (p *pdfReport) title(title, subtitle string) {
	for _, line := range p.doc.font.wrap(title, 28, pdfContentWidth) {
		p.page.text(pdfMargin, p.y, 28, pdfInk, line)
		p.y += 35
	}
	p.y += 6
	p.paragraph(subtitle, pdfMuted)
	p.y += 6
}
func (p *pdfReport) scoreCards(runs []Run) {
	if len(runs) > 2 {
		runs = runs[:2]
	}
	count := len(runs)
	if count == 0 {
		return
	}
	p.ensure(111)
	gap := 14.0
	w := (pdfContentWidth - gap*float64(count-1)) / float64(count)
	for i, r := range runs {
		x := pdfMargin + float64(i)*(w+gap)
		score, unit := pdfScore(r)
		p.page.rect(x, p.y, w, 100, pdfPaper)
		p.page.text(x+15, p.y+13, 9, pdfMuted, strings.ToUpper(pdfPhaseName(r)))
		p.page.text(x+15, p.y+32, 25, pdfInk, score)
		for j, line := range p.doc.font.wrap(unit, 8, w-30) {
			p.page.text(x+15, p.y+68+float64(j)*11, 8, pdfMuted, line)
		}
	}
	p.y += 125
}
func (p *pdfReport) overview(r Run) {
	p.newPage("Findings report")
	p.title("Thermal findings", r.Host+"  /  "+r.Created.Format("02 Jan 2006, 15:04 MST"))
	p.status(r)
	if r.Host == "demo" || strings.Contains(r.Workload, "synthetic") {
		p.paragraph("SYNTHETIC DEMO — illustrative data, not measurements from this computer.", pdfAmber)
	}
	p.scoreCards(pdfRuns(r))
	p.heading("Findings")
	for _, f := range pdfFindings(r) {
		p.finding(f)
	}
	p.heading("Next step")
	action := "Repeat the same workload under matching power, profile and room conditions to establish whether the result is repeatable."
	for _, phase := range pdfRuns(r) {
		if phase.Status == "refused" {
			action = "Configure temperature monitoring for the refused test, then repeat it. Review the completed test independently."
			break
		}
	}
	p.paragraph(action, pdfInk)
	p.heading("Test conditions")
	p.paragraph(fmt.Sprintf("Stage: %s  /  Profile: %s  /  Power: %s  /  Ambient: %s", r.Stage, r.Profile, r.PowerSource, pdfNumber(r.Ambient, " °C", false)), pdfMuted)
	p.paragraph("Findings use recorded evidence and generic screening thresholds (CPU 85 °C / GPU 80 °C), not manufacturer fault limits. Missing readings remain unknown.", pdfMuted)
}
func (p *pdfReport) metricRow(items [][2]string) {
	p.ensure(81)
	w := pdfContentWidth / float64(len(items))
	for i, item := range items {
		x := pdfMargin + float64(i)*w
		p.page.text(x, p.y, 8, pdfMuted, strings.ToUpper(item[0]))
		p.page.text(x, p.y+21, 23, pdfInk, item[1])
	}
	p.page.line(pdfMargin, p.y+65, pdfWidth-pdfMargin, p.y+65, 0.5, pdfRule)
	p.y += 88
}
func (p *pdfReport) chart(r Run, id string) {
	p.ensure(183)
	x, y, w, h := pdfMargin+32, p.y+8, pdfContentWidth-40, 112.0
	low, high, end := math.Inf(1), math.Inf(-1), 0.0
	for _, s := range r.Samples {
		end = math.Max(end, s.Seconds)
		for _, d := range s.Devices {
			if d.ID == id && d.Temp != nil {
				low = math.Min(low, *d.Temp)
				high = math.Max(high, *d.Temp)
			}
		}
	}
	if math.IsInf(low, 1) {
		p.page.rect(pdfMargin, p.y, pdfContentWidth, 115, pdfPaper)
		p.page.text(pdfMargin+16, p.y+44, 11, pdfMuted, "No target temperature data for this test.")
		p.y += 139
		return
	}
	low = math.Floor((low-3)/5) * 5
	high = math.Ceil((high+3)/5) * 5
	for i := 0; i < 3; i++ {
		yy := y + float64(i)*h/2
		p.page.line(x, yy, x+w, yy, 0.5, pdfRule)
		p.page.text(pdfMargin, yy-5, 8, pdfMuted, fmt.Sprintf("%.0f°", high-float64(i)*(high-low)/2))
	}
	for i := 0; i < 3; i++ {
		xx := x + float64(i)*w/2
		p.page.text(xx-8, y+h+10, 8, pdfMuted, fmt.Sprintf("%.0fs", end*float64(i)/2))
	}
	previous := false
	px, py := 0.0, 0.0
	for _, s := range r.Samples {
		var v *float64
		for _, d := range s.Devices {
			if d.ID == id {
				v = d.Temp
				break
			}
		}
		if v == nil {
			previous = false
			continue
		}
		xx := x + w*s.Seconds/math.Max(end, 1)
		yy := y + h*(high-*v)/(high-low)
		if previous {
			p.page.line(px, py, xx, yy, 1.5, pdfAccent)
		}
		p.page.rect(xx-1, yy-1, 2, 2, pdfAccent)
		px, py, previous = xx, yy, true
	}
	p.y += 162
}
func (p *pdfReport) row(label, value string) {
	labels := p.doc.font.wrap(label, 10, 190)
	values := p.doc.font.wrap(value, 10, pdfContentWidth-210)
	lines := max(len(labels), len(values))
	h := float64(lines)*15 + 15
	if h <= pdfHeight-162 {
		p.ensure(h)
	}
	for i := 0; i < lines; i++ {
		p.ensure(15)
		if p.err != nil {
			return
		}
		if i < len(labels) {
			p.page.text(pdfMargin, p.y, 10, pdfMuted, labels[i])
		}
		if i < len(values) {
			p.page.text(pdfMargin+210, p.y, 10, pdfInk, values[i])
		}
		p.y += 15
	}
	p.y += 15
}
func pdfNextStep(r Run) string {
	if r.Status == "refused" {
		return "Resolve the temperature or monitoring condition listed below, then repeat this test."
	}
	if r.Status == "stopped" {
		return "Let the machine cool and resolve the monitoring or temperature stop before repeating the test."
	}
	if r.Status == "unavailable" || r.Status == "failed" || r.Status == "skipped" {
		return "Resolve the reported limitation before treating this test as a completed benchmark."
	}
	for _, note := range Recommend(r) {
		if strings.HasPrefix(note, "Stage ") {
			return note
		}
	}
	return "Repeat the same workload with matching duration, power source, profile and room conditions."
}
func (p *pdfReport) details(r Run) {
	p.newPage("Measurement detail")
	p.title(pdfPhaseName(r), r.Workload+"  /  "+r.Stage+"  /  "+r.Status)
	score, unit := pdfScore(r)
	p.row("Throughput", score+" "+unit)
	p.row("Captured / requested", fmt.Sprintf("%.1f s / %.1f s", r.Elapsed, r.Duration))
	id, s := TargetStats(r, "")
	measured := r.Operations > 0 || r.GPU != nil && r.GPU.Rate() != nil || r.Workload != "cpu-sha256-v1" && r.Workload != "gpu-integer-v1"
	if measured {
		peak := Summarize(r, false)[id].Peak
		p.y += 8
		p.metricRow([][2]string{{"Sustained temperature", pdfNumber(s.Mean, " °C", false)}, {"Peak temperature", pdfNumber(peak, " °C", false)}, {"Sustained power", pdfNumber(s.Power, " W", false)}})
		p.ensure(220)
		p.heading("Temperature over time")
		if id != "" {
			p.paragraph(s.Name, pdfMuted)
		}
		p.chart(r, id)
		p.row("Clock / throttling", pdfNumber(s.Clock, " MHz", false)+" / "+throttle(s))
	} else {
		p.finding(pdfFinding{"No workload result", "This test did not run. Temperature and performance cannot be assessed from this phase.", true})
	}
	if r.GPU != nil {
		p.row("Compute device", r.GPU.Device+" / "+r.GPU.Backend)
	}
	if r.Workers > 0 {
		p.row("CPU workers", fmt.Sprint(r.Workers))
	}
	p.y += 4
	p.heading("Next step")
	p.paragraph(pdfNextStep(r), pdfInk)
	var notes []string
	for _, warning := range uniquePDFStrings(r.Warnings) {
		// Driver-wide CPU notices belong in the CPU phase, not in GPU findings.
		if r.Workload == "gpu-integer-v1" && strings.HasPrefix(warning, "CPU ") {
			continue
		}
		notes = append(notes, warning)
	}
	if len(notes) > 0 {
		p.heading("Measurement notes")
		for _, note := range notes {
			p.paragraph(note, pdfMuted)
		}
	}
	if r.Notes != "" {
		p.heading("Recorded notes")
		p.paragraph(r.Notes, pdfInk)
	}
	if measured {
		p.ensure(50)
		p.paragraph("Method: sustained = final 25% of captured time; peak = whole test. Chart gaps preserve missing readings. Power channels may overlap and are not summed.", pdfMuted)
	}
}

func uniquePDFStrings(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range xs {
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out
}

func (p *pdfReport) comparison(a, b Run) {
	p.newPage("Comparison report")
	p.title("Before / after", a.Host+"  /  "+a.Stage+" → "+b.Stage)
	if a.Host == "demo" || b.Host == "demo" {
		p.paragraph("SYNTHETIC DEMO — illustrative data, not measurements from this computer.", pdfAmber)
	}
	p.paragraph("Changes are after minus before. Assess workload and condition warnings before attributing a difference to an intervention.", pdfMuted)
	if len(a.Phases) > 0 || len(b.Phases) > 0 {
		if len(a.Phases) == 0 || len(b.Phases) == 0 {
			p.finding(pdfFinding{"Runs are not comparable", "A benchmark suite cannot be compared directly with an individual recording.", true})
			return
		}
		for _, phase := range a.Phases {
			found := false
			for _, next := range b.Phases {
				if phase.Workload == next.Workload {
					p.comparePhase(phase, next)
					found = true
					break
				}
			}
			if !found {
				p.paragraph("Phase missing after: "+phase.Workload, pdfAmber)
			}
		}
		for _, next := range b.Phases {
			found := false
			for _, phase := range a.Phases {
				if phase.Workload == next.Workload {
					found = true
				}
			}
			if !found {
				p.paragraph("New phase after: "+next.Workload, pdfAmber)
			}
		}
	} else {
		p.comparePhase(a, b)
	}
	p.heading("Conclusion")
	p.paragraph("These measurements describe a change, not its cause. Review sustained clocks and power alongside temperature, then repeat under comparable conditions.", pdfInk)
}
func (p *pdfReport) comparePhase(a, b Run) {
	p.heading(pdfPhaseName(a))
	p.row("Run status", a.Status+" → "+b.Status)
	c := Compare(a, b)
	if c.ThroughputDelta != nil {
		p.row("CPU throughput change", signed(c.ThroughputDelta, " hashes/s"))
	}
	if c.GPUThroughputDelta != nil {
		p.row("GPU throughput change", signed(c.GPUThroughputDelta, " iterations/s"))
	}
	for _, d := range c.Devices {
		p.ensure(95)
		p.paragraph(d.Name, pdfInk)
		p.row("Sustained temperature", value(d.Before.Mean, " °C")+" → "+value(d.After.Mean, " °C")+" ("+signed(d.Temperature, " °C")+")")
		p.row("Sustained clock", value(d.Before.Clock, " MHz")+" → "+value(d.After.Clock, " MHz")+" ("+signed(d.Clock, " MHz")+")")
		p.row("Sustained power", value(d.Before.Power, " W")+" → "+value(d.After.Power, " W")+" ("+signed(d.Power, " W")+")")
		p.row("Thermal throttling", throttle(d.Before)+" → "+throttle(d.After))
	}
	notes := append(append(append([]string{}, c.Warnings...), a.Warnings...), b.Warnings...)
	if len(notes) > 0 {
		p.heading("Comparison notes")
		for _, note := range uniquePDFStrings(notes) {
			p.paragraph(note, pdfMuted)
		}
	}
}

func WriteReportPDF(w io.Writer, before Run, after *Run) error {
	font, err := newPDFFont()
	if err != nil {
		return err
	}
	p := pdfReport{doc: pdfDocument{font: font}}
	if after != nil {
		p.comparison(before, *after)
	} else {
		p.overview(before)
		for _, r := range pdfRuns(before) {
			p.details(r)
		}
	}
	if p.err != nil {
		return p.err
	}
	return p.doc.write(w)
}
func SaveReportPDF(path string, before Run, after *Run) error {
	if !strings.EqualFold(filepath.Ext(path), ".pdf") {
		return fmt.Errorf("PDF output must end in .pdf")
	}
	var b bytes.Buffer
	if err := WriteReportPDF(&b, before, after); err != nil {
		return err
	}
	return saveExclusive(path, b.Bytes())
}
