package thermal

import (
	"fmt"
	"io"
	"strings"
)

// compactRate keeps large compute counters readable without changing their units.
func compactRate(v float64) string {
	for _, scale := range []struct {
		n      float64
		suffix string
	}{
		{1e12, "T"}, {1e9, "G"}, {1e6, "M"}, {1e3, "k"},
	} {
		if v >= scale.n {
			return fmt.Sprintf("%.2f %s", v/scale.n, scale.suffix)
		}
	}
	return fmt.Sprintf("%.1f", v)
}

// BenchmarkSummary shows target-specific results. Full sensor tables, context,
// and all warnings remain available through Report and the saved reports.
func BenchmarkSummary(w io.Writer, r Run) {
	d := newDisplay(w)
	d.header("BENCHMARK / "+strings.ToUpper(r.Status), r.Created.UTC().Format("2006-01-02 15:04 UTC")+" / "+r.Stage)
	phases := r.Phases
	if len(phases) == 0 {
		phases = []Run{r}
	}
	throttled, missing, incomplete := false, false, false
	for _, phase := range phases {
		name := "CPU"
		if phase.Workload == "gpu-integer-v1" {
			name = "GPU"
		}
		d.section(name + " / " + strings.ToUpper(phase.Status))
		if phase.GPU != nil {
			d.line(phase.GPU.Device)
		}
		score := ""
		if phase.Workload == "cpu-sha256-v1" && phase.Operations > 0 && phase.Elapsed > 0 {
			score = compactRate(float64(phase.Operations)/phase.Elapsed) + " hashes/s"
		} else if phase.GPU != nil && phase.GPU.Rate() != nil {
			score = compactRate(*phase.GPU.Rate()) + " verified iterations/s"
		}
		if score != "" {
			d.line(fmt.Sprintf("Score         %s / %.1f s", score, phase.Elapsed))
		}
		if phase.Status != "complete" {
			incomplete = true
			if len(phase.Warnings) > 0 {
				reason := phase.Warnings[len(phase.Warnings)-1]
				for _, warning := range phase.Warnings {
					if strings.Contains(strings.ToLower(warning), "temperature limit") ||
						strings.HasPrefix(warning, "CPU temperature unavailable;") ||
						strings.HasPrefix(warning, "Temperature unavailable for the selected GPU;") {
						reason = warning
						break
					}
				}
				d.bullet(reason)
			}
			if score == "" {
				continue
			}
			d.line("Partial score; requested test duration was not completed.")
		}
		id, sustained := primaryTargetStats(phase)
		all := Summarize(phase, false)[id]
		d.line("Temperature   " + value(all.Mean, " C") + " mean / " + value(all.Peak, " C") + " peak")
		d.line("Sustained     " + value(sustained.Mean, " C") + " / " + value(sustained.Power, " W") + " / " + value(sustained.Clock, " MHz"))
		d.line("Throttling    " + throttle(all) + " overall / " + throttle(sustained) + " sustained")
		throttled = throttled || all.ThrottleSamples > 0
		missing = missing || sustained.Mean == nil
		for _, warning := range phase.Warnings {
			if strings.HasPrefix(warning, "Unmonitored mode") {
				d.bullet(warning)
			}
		}
	}
	d.section("NEXT STEP")
	switch {
	case incomplete:
		d.line("Check the incomplete test's notes; review completed tests separately.")
	case missing:
		d.line("Configure target temperature monitoring before assessing thermals.")
	case throttled:
		d.line("Repeat under matching conditions to check sustained throttling.")
	default:
		d.line("Use the PDF findings to review this run or compare a matching test.")
	}
	if throttled && incomplete {
		d.line("Thermal throttling was also observed in a completed or partial test.")
	}
	d.line("Sustained = final 25% of each test. Scores measure compute, not FPS.")
	d.line("Full details: saved PDF, or thermal report <run.json>. Use --verbose for tables.")
	fmt.Fprintln(w)
}

func gpuRateText(g *GPUResult) string {
	if g.Rate() == nil {
		return "unknown verified iterations/s"
	}
	return compactRate(*g.Rate()) + " verified iterations/s"
}
