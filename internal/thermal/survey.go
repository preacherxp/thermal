package thermal

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

const LenovoSpecURL = "https://psref.lenovo.com/syspool/Sys/PDF/datasheet/Legion_Pro_7_16IRX8_datasheet_EN.pdf"
const LenovoTIMURL = "https://seservice.lenovo.com/static_file/tape/tape.html"
const LegionReviewURL = "https://thehikaku.net/pc/lenovo/23Legion-Pro-7i-Gen8.html"

var Materials = []string{"unknown", "paste", "liquid-metal", "phase-change", "pad", "other"}

type MaterialInfo struct {
	FactoryDetail string `json:"factory_detail,omitempty"`
	Current       string `json:"current"`
	Factory       string `json:"factory,omitempty"`
	Source        string `json:"factory_source,omitempty"`
}
type GPUExpectation struct {
	SharedWithCPU bool         `json:"shared_with_cpu,omitempty"`
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	RatedWatts    *float64     `json:"rated_w,omitempty"`
	Source        string       `json:"rated_source,omitempty"`
	Material      MaterialInfo `json:"material"`
}
type ScoreReference struct {
	Benchmark string  `json:"benchmark"`
	Component string  `json:"component"`
	Profile   string  `json:"profile"`
	Score     float64 `json:"score"`
	Source    string  `json:"source"`
	Evidence  string  `json:"evidence"`
}
type Survey struct {
	Created     time.Time        `json:"created"`
	Hardware    HardwareInfo     `json:"hardware"`
	GPUs        []GPUExpectation `json:"gpu_expectations"`
	CPUMaterial MaterialInfo     `json:"cpu_material"`
	Scores      []ScoreReference `json:"score_references"`
	Profile     string           `json:"profile"`
	Mode        string           `json:"mode"`
	CatalogDate string           `json:"catalog_checked"`
}

func BuildSurvey(h HardwareInfo, profile string) Survey {
	s := Survey{Created: time.Now().UTC(), Hardware: h, CPUMaterial: MaterialInfo{Current: "unknown"}, GPUs: []GPUExpectation{}, Scores: []ScoreReference{}, Profile: profile, Mode: "auto", CatalogDate: "2026-09-06"}
	machine := strings.ToLower(h.Model + " " + h.Family)
	legion := strings.Contains(strings.ToLower(h.Manufacturer), "lenovo") && (strings.Contains(machine, "16irx8h") || strings.HasPrefix(strings.ToUpper(h.Model), "82WQ"))
	if legion {
		s.CPUMaterial.Factory = "liquid-metal"
		s.CPUMaterial.Source = LenovoTIMURL
	}
	hasDiscrete := false
	for _, g := range h.GPUs {
		if !integratedGPU(g.Name) {
			hasDiscrete = true
		}
	}
	for _, g := range h.GPUs {
		if hasDiscrete && integratedGPU(g.Name) {
			continue
		}
		x := GPUExpectation{ID: g.ID, Name: g.Name, SharedWithCPU: integratedGPU(g.Name), Material: MaterialInfo{Current: "unknown"}}
		laptop4080 := strings.Contains(strings.ToLower(g.Name), "4080") && (strings.Contains(strings.ToLower(g.Name), "laptop") || strings.Contains(strings.ToLower(g.Name), "notebook"))
		if legion && laptop4080 {
			x.RatedWatts = Number(175)
			x.Source = LenovoSpecURL
			x.Material.Factory = "phase-change"
			x.Material.FactoryDetail = "PTM795X series"
			x.Material.Source = LenovoTIMURL
			if strings.Contains(strings.ToLower(h.CPU), "13900hx") && len(s.Scores) == 0 {
				s.Scores = append(s.Scores, ScoreReference{"3DMark Time Spy graphics", "gpu", "performance", 18822, LegionReviewURL, "single review unit; indicative, not a guaranteed target"},
					ScoreReference{"Cinebench R23 multi-core", "cpu", "performance", 27237, LegionReviewURL, "single review unit; indicative, not a guaranteed target"},
					ScoreReference{"Cinebench R23 multi-core", "cpu", "balanced", 21080, LegionReviewURL, "single review unit; indicative, not a guaranteed target"}, ScoreReference{"3DMark Time Spy graphics", "gpu", "balanced", 11668, LegionReviewURL, "single review unit; indicative, not a guaranteed target"})
			}
		}
		s.GPUs = append(s.GPUs, x)
	}
	return s
}
func validMaterial(value string) bool { return slices.Contains(Materials, value) }
func resolveMaterial(value string, info MaterialInfo) (MaterialInfo, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "factory" {
		if info.Factory == "" {
			return info, errors.New("no factory material reference for this device")
		}
		info.Current = info.Factory
		return info, nil
	}
	if !validMaterial(value) {
		return info, fmt.Errorf("choose %s, or factory when available", strings.Join(Materials, ", "))
	}
	info.Current = value
	return info, nil
}
func PositiveNumber(text string, max float64) (float64, error) {
	v, e := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if e != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > max {
		return 0, fmt.Errorf("enter a number greater than 0 and at most %.0f", max)
	}
	return v, nil
}
func ApplySurveyInputs(s *Survey, tgp, cpuTIM, gpuTIM, benchmark, score, source string) error {
	if tgp != "" {
		if len(s.GPUs) == 0 || s.GPUs[0].SharedWithCPU {
			return errors.New("--gpu-watts requires a detected discrete GPU")
		}
		v, e := PositiveNumber(tgp, 2000)
		if e != nil {
			return e
		}
		s.GPUs[0].RatedWatts = Number(v)
		s.GPUs[0].Source = "user-provided"
	}
	var e error
	if cpuTIM != "" {
		s.CPUMaterial, e = resolveMaterial(cpuTIM, s.CPUMaterial)
		if e != nil {
			return e
		}
	}
	if gpuTIM != "" {
		if len(s.GPUs) == 0 || s.GPUs[0].SharedWithCPU {
			return errors.New("--gpu-material requires a separately cooled discrete GPU; use --cpu-material for integrated graphics")
		}
		s.GPUs[0].Material, e = resolveMaterial(gpuTIM, s.GPUs[0].Material)
		if e != nil {
			return e
		}
	}
	if (benchmark == "") != (score == "") {
		return errors.New("--expected-benchmark and --expected-score must be provided together")
	}
	if benchmark != "" {
		v, e := PositiveNumber(score, 1e15)
		if e != nil {
			return e
		}
		if source == "" {
			source = "user-provided; unverified"
		}
		s.Scores = append(s.Scores, ScoreReference{benchmark, "user-selected", s.Profile, v, source, "user-provided target"})
	}
	return nil
}
func promptAnswer(ctx context.Context, scan *bufio.Scanner, w io.Writer, label, defaultValue string) (string, error) {
	fmt.Fprintf(w, "  %s [%s]: ", label, defaultValue)
	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		if scan.Scan() {
			ch <- result{strings.TrimSpace(scan.Text()), nil}
		} else {
			e := scan.Err()
			if e == nil {
				e = io.EOF
			}
			ch <- result{"", e}
		}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return "", fmt.Errorf("survey input ended; no test started: %w", r.err)
		}
		if r.text == "" {
			return defaultValue, nil
		}
		return r.text, nil
	}
}
func PromptSurvey(ctx context.Context, in io.Reader, out io.Writer, s *Survey) error {
	scan := bufio.NewScanner(in)
	s.Mode = "interactive"
	d := newDisplay(out)
	d.section("1 / 3  EXPECTED GPU WATTAGE")
	d.bullet("Use the OEM-rated maximum for this exact device. Idle draw and a configurable limit are not sustained targets.")
	for i := range s.GPUs {
		gpu := &s.GPUs[i]
		if gpu.SharedWithCPU {
			continue
		}
		current := "unknown"
		if gpu.RatedWatts != nil {
			current = fmt.Sprintf("%.1f", *gpu.RatedWatts)
		}
		for {
			answer, e := promptAnswer(ctx, scan, out, gpu.Name+" rated W", current)
			if e != nil {
				return e
			}
			if answer == "unknown" {
				gpu.RatedWatts = nil
				gpu.Source = "unknown"
				break
			}
			v, e := PositiveNumber(answer, 2000)
			if e != nil {
				d.line(e.Error())
				continue
			}
			if gpu.RatedWatts == nil || v != *gpu.RatedWatts {
				gpu.Source = "user-provided"
			}
			gpu.RatedWatts = Number(v)
			break
		}
	}
	d.section("2 / 3  THERMAL INTERFACE")
	d.bullet("Types: paste (grease), liquid-metal, phase-change (e.g. PTM), pad, other, unknown. Pads for memory/VRMs are not interchangeable with CPU/GPU die material.")
	d.bullet("Factory entries are references, not detection of what is installed now. Enter factory only if unchanged.")
	askMaterial := func(label string, m *MaterialInfo) error {
		if m.Factory != "" {
			d.line(label + " factory reference: " + m.Factory)
		}
		for {
			answer, e := promptAnswer(ctx, scan, out, label+" current material", m.Current)
			if e != nil {
				return e
			}
			updated, e := resolveMaterial(answer, *m)
			if e != nil {
				d.line(e.Error())
				continue
			}
			*m = updated
			return nil
		}
	}
	if e := askMaterial("CPU", &s.CPUMaterial); e != nil {
		return e
	}
	for i := range s.GPUs {
		if s.GPUs[i].SharedWithCPU {
			continue
		}
		if e := askMaterial(s.GPUs[i].Name, &s.GPUs[i].Material); e != nil {
			return e
		}
	}
	d.section("3 / 3  BENCHMARK EXPECTATIONS")
	profile, e := promptAnswer(ctx, scan, out, "Current thermal profile (label only)", s.Profile)
	if e != nil {
		return e
	}
	s.Profile = profile
	for i := range s.Scores {
		if s.Scores[i].Evidence == "user-provided target" {
			s.Scores[i].Profile = profile
		}
	}
	d.bullet("Published references are shown above. Thermal's built-in SHA-256 hashes/s is a different metric; it has no validated model score database.")
	bench, e := promptAnswer(ctx, scan, out, "Optional additional benchmark name (or none)", "none")
	if e != nil {
		return e
	}
	if strings.EqualFold(bench, "none") {
		return nil
	}
	for {
		text, e := promptAnswer(ctx, scan, out, "Expected score", "unknown")
		if e != nil {
			return e
		}
		v, e := PositiveNumber(text, 1e15)
		if e != nil {
			d.line(e.Error())
			continue
		}
		src, e := promptAnswer(ctx, scan, out, "Score source URL or note", "user-provided; unverified")
		if e != nil {
			return e
		}
		s.Scores = append(s.Scores, ScoreReference{bench, "user-selected", s.Profile, v, src, "user-provided target"})
		return nil
	}
}
func PrintSurvey(w io.Writer, s Survey) {
	d := newDisplay(w)
	d.header("PRE-RUN SURVEY", s.Hardware.Manufacturer+" "+s.Hardware.Model+" "+s.Hardware.Family)
	d.line("CPU  " + s.Hardware.CPU)
	d.line(fmt.Sprintf("%d cores / %d threads  ·  RAM %s", s.Hardware.PhysicalCores, s.Hardware.LogicalCPUs, value(s.Hardware.RAMGiB, " GiB")))
	for _, g := range s.Hardware.GPUs {
		d.line("GPU  " + g.Name)
	}
	d.section("1 / 3  EXPECTED GPU WATTAGE")
	for _, g := range s.GPUs {
		if g.SharedWithCPU {
			d.line(g.Name + " · integrated; no independent rated board wattage")
			continue
		}
		d.line(g.Name + "  ·  rated maximum " + value(g.RatedWatts, " W"))
		for _, device := range s.Hardware.GPUs {
			if device.ID == g.ID {
				d.line("Driver max " + value(device.DriverMaxWatts, " W") + " / enforced now " + value(device.EnforcedWatts, " W"))
			}
		}
	}
	d.bullet("Rated maximum is not idle or guaranteed sustained draw; workload, profile, AC power and shared CPU/GPU budgets matter.")
	d.section("2 / 3  THERMAL INTERFACE")
	material := func(label string, m MaterialInfo) {
		line := label + " current: " + m.Current
		if m.Factory != "" {
			line += " · factory: " + m.Factory
			if m.FactoryDetail != "" {
				line += " (" + m.FactoryDetail + ")"
			}
		}
		d.line(line)
	}
	material("CPU", s.CPUMaterial)
	for _, g := range s.GPUs {
		if !g.SharedWithCPU {
			material(g.Name, g.Material)
		}
	}
	d.bullet("Types: paste/grease, liquid-metal, phase-change (PTM), pad, other, unknown. Installed material cannot be detected in software.")
	d.bullet("Liquid metal is conductive. Follow model-specific service instructions; factory references may differ after repairs.")
	d.section("3 / 3  EXPECTED BENCHMARK SCORES")
	d.line("Selected profile: " + s.Profile)
	if len(s.Scores) == 0 {
		d.line("No vetted score reference for this system; provide a named target or use your own baseline.")
	}
	for _, r := range s.Scores {
		d.line(fmt.Sprintf("%-30s %7.0f · %s", r.Benchmark, r.Score, r.Profile))
	}
	d.bullet("Published scores are single-unit reference results, not guaranteed targets. Match the benchmark and profile.")
	d.bullet("Built-in CPU SHA-256 hashes/s has no validated model reference and cannot be converted to Cinebench or Time Spy scores.")
	if s.CPUMaterial.Source != "" {
		d.line("Sources: Lenovo PSREF / service table; the比較 review. URLs and provenance: thermal survey --json")
	}
	d.warnings(s.Hardware.Warnings)
}

func integratedGPU(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "apple ") || strings.Contains(n, "intel") && (strings.Contains(n, "uhd") || strings.Contains(n, "iris") || strings.Contains(n, "hd graphics"))
}
