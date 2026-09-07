package thermal

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func legionHardware() HardwareInfo {
	return HardwareInfo{Manufacturer: "LENOVO", Model: "82WQ", Family: "Legion Pro 7 16IRX8H", CPU: "Intel Core i9-13900HX", PhysicalCores: 24, LogicalCPUs: 32, GPUs: []GPUInfo{{ID: "nv:0", Name: "NVIDIA GeForce RTX 4080 Laptop GPU", DriverMaxWatts: Number(175)}}}
}
func TestSurveyExactModelReferences(t *testing.T) {
	s := BuildSurvey(legionHardware(), "performance")
	if s.GPUs[0].RatedWatts == nil || *s.GPUs[0].RatedWatts != 175 || s.GPUs[0].Source != LenovoSpecURL {
		t.Fatal(s)
	}
	if s.CPUMaterial.Current != "unknown" || s.CPUMaterial.Factory != "liquid-metal" || s.GPUs[0].Material.Factory != "phase-change" {
		t.Fatal("factory data must not assert installed material")
	}
	if len(s.Scores) != 4 || s.Scores[0].Benchmark != "3DMark Time Spy graphics" || s.Scores[0].Score != 18822 {
		t.Fatal(s.Scores)
	}
	for _, r := range s.Scores {
		if r.Source == "" || !strings.Contains(r.Evidence, "single review") {
			t.Fatal("missing score provenance")
		}
	}
}
func TestUnknownModelDoesNotInheritLaptopTGP(t *testing.T) {
	h := legionHardware()
	h.Manufacturer = "Other"
	h.Model = "other"
	h.Family = "other"
	s := BuildSurvey(h, "unknown")
	if s.GPUs[0].RatedWatts != nil || len(s.Scores) != 0 || s.CPUMaterial.Factory != "" {
		t.Fatal("generic GPU name or driver max used as OEM expectation")
	}
	h = legionHardware()
	h.CPU = "Intel Core i7"
	if len(BuildSurvey(h, "unknown").Scores) != 0 {
		t.Fatal("wrong CPU got score references")
	}
	h = legionHardware()
	h.GPUs[0].Name = "NVIDIA GeForce RTX 4080"
	s = BuildSurvey(h, "unknown")
	if s.GPUs[0].RatedWatts != nil || len(s.Scores) != 0 {
		t.Fatal("desktop GPU got laptop expectations")
	}
}
func TestIntegratedGPUIsNotSeparatelyRepasted(t *testing.T) {
	h := legionHardware()
	h.GPUs = append([]GPUInfo{{ID: "integrated", Name: "Intel(R) UHD Graphics"}}, h.GPUs...)
	s := BuildSurvey(h, "unknown")
	if len(s.Hardware.GPUs) != 2 || len(s.GPUs) != 1 || s.GPUs[0].ID != "nv:0" {
		t.Fatal(s)
	}
}
func TestSurveyInputsValidateAndRetainProvenance(t *testing.T) {
	s := BuildSurvey(legionHardware(), "performance")
	if e := ApplySurveyInputs(&s, "150", "factory", "paste", "My test v1", "1234", "local baseline"); e != nil {
		t.Fatal(e)
	}
	if s.CPUMaterial.Current != "liquid-metal" || s.GPUs[0].Material.Current != "paste" || s.GPUs[0].Material.Factory != "phase-change" {
		t.Fatal(s)
	}
	if *s.GPUs[0].RatedWatts != 150 || s.GPUs[0].Source != "user-provided" {
		t.Fatal(s)
	}
	last := s.Scores[len(s.Scores)-1]
	if last.Source != "local baseline" || last.Score != 1234 {
		t.Fatal(last)
	}
	for _, v := range []string{"NaN", "Inf", "-1", "0", "hello", "999999"} {
		if _, e := PositiveNumber(v, 2000); e == nil {
			t.Fatal("accepted " + v)
		}
	}
	if e := ApplySurveyInputs(&s, "", "", "", "Cinebench", "", ""); e == nil {
		t.Fatal("unnamed score accepted")
	}
	unknown := BuildSurvey(HardwareInfo{}, "unknown")
	if e := ApplySurveyInputs(&unknown, "", "factory", "", "", "", ""); e == nil {
		t.Fatal("confirmed nonexistent factory data")
	}
}
func TestInteractiveSurvey(t *testing.T) {
	s := BuildSurvey(legionHardware(), "unknown")
	var out bytes.Buffer
	// Keep OEM TGP, confirm factory CPU material, record a GPU change, select profile, no extra score.
	input := strings.NewReader("\nfactory\npaste\nperformance\nnone\n")
	if e := PromptSurvey(context.Background(), input, &out, &s); e != nil {
		t.Fatal(e)
	}
	if s.Mode != "interactive" || s.Profile != "performance" || s.CPUMaterial.Current != "liquid-metal" || s.GPUs[0].Material.Current != "paste" {
		t.Fatal(s)
	}
	if !strings.Contains(out.String(), "1 / 3") || !strings.Contains(out.String(), "3 / 3") {
		t.Fatal(out.String())
	}
}
func TestSurveyEOFAndCancellation(t *testing.T) {
	s := BuildSurvey(legionHardware(), "unknown")
	if e := PromptSurvey(context.Background(), strings.NewReader(""), &bytes.Buffer{}, &s); e == nil {
		t.Fatal("EOF must not silently authorize a run")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := PromptSurvey(ctx, strings.NewReader(""), &bytes.Buffer{}, &s); e == nil {
		t.Fatal("cancelled prompt accepted")
	}
}
func TestSurveyStoredWithRun(t *testing.T) {
	s := BuildSurvey(legionHardware(), "balanced")
	o := testOptions()
	o.Preparation = &s
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, e := Capture(ctx, &fakeReader{samples: []Sample{sensor(50)}}, o, nil)
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	var restored Run
	if e = json.Unmarshal(b, &restored); e != nil {
		t.Fatal(e)
	}
	if restored.Preparation == nil || restored.Preparation.Scores[0].Score != 18822 {
		t.Fatal("survey lost in saved run")
	}
}
