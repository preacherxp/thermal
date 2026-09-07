package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"thermal-cli/internal/thermal"
)

// Opt-in integration test: invokes the real default command, including monitored
// CPU/GPU load where hardware is available. Ordinary unit tests use fake sensors.
func TestNativeDefaultBenchmark(t *testing.T) {
	if os.Getenv("THERMAL_TEST_BENCHMARK") != "1" {
		t.Skip("set THERMAL_TEST_BENCHMARK=1 after building to test the default benchmark")
	}
	exe := "thermal"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	path, err := filepath.Abs(filepath.Join("..", "..", "dist", runtime.GOOS+"-"+runtime.GOARCH, exe))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path)
	cmd.Env = append(os.Environ(), "THERMAL_DATA_DIR="+dir, "THERMAL_EXTERNAL_PROVIDERS=0")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("default command timed out: %s", output)
	}
	if err != nil {
		if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 1 {
			t.Fatalf("%v: %s", err, output)
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("expected one JSON report: %v %v\n%s", files, err, output)
	}
	r, err := thermal.Load(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if r.Workload != "cpu-gpu-suite-v1" || len(r.Phases) != 2 || r.Duration != 60 {
		t.Fatalf("unexpected default run: %+v", r)
	}
	if r.Phases[0].Workload != "cpu-sha256-v1" || r.Phases[1].Workload != "gpu-integer-v1" {
		t.Fatal("missing CPU or GPU phase")
	}
	for _, p := range r.Phases {
		if p.Duration != 30 {
			t.Fatal("wrong phase duration")
		}
		// A native AppleSMC reading must lead to an attempted monitored test.
		// This catches regressions where macOS silently becomes unsupported again.
		if runtime.GOOS == "darwin" && len(p.Samples) > 0 {
			if p.Workload == "cpu-sha256-v1" && thermal.Guard(p.Samples[0], p.StopTemp, false) == "" && p.Status == "refused" {
				t.Fatalf("readable macOS CPU refused: %v", p.Warnings)
			}
			for _, d := range p.Samples[0].Devices {
				if (d.ID == "smc:gpu" || d.ID == "smc:TCMz" || d.ID == "smc:TCMb") && d.Temp != nil && p.Status == "complete" && d.Power == nil {
					t.Fatalf("native Apple Silicon %s wattage missing: %v", d.Kind, p.Warnings)
				}
				if d.ID == "smc:gpu" && d.Temp != nil && p.Workload == "gpu-integer-v1" && p.Status == "unavailable" {
					t.Fatalf("Apple Silicon GPU backend unavailable: %v", p.Warnings)
				}
			}
		}
		if p.Status == "complete" {
			if p.Workload == "cpu-sha256-v1" && p.Operations == 0 {
				t.Fatal("CPU benchmark did no work")
			}
			if p.Workload == "gpu-integer-v1" && (p.GPU == nil || p.GPU.Rate() == nil) {
				t.Fatal("GPU benchmark did no verified work")
			}
		}
	}
	if (r.Status == "complete") != (cmd.ProcessState.ExitCode() == 0) {
		t.Fatal("exit status disagrees with report")
	}
	pdf := defaultPDFPath(files[0])
	assertPDF(t, pdf)
	if !strings.Contains(string(output), pdf) {
		t.Fatal("PDF path not printed")
	}
	if strings.Contains(string(output), "POWER LIMITS") || !strings.Contains(string(output), "BENCHMARK /") {
		t.Fatalf("default benchmark did not use compact output: %s", output)
	}
	png := strings.TrimSuffix(files[0], ".json") + ".png"
	assertPNG(t, png)
	if !strings.Contains(string(output), files[0]) || !strings.Contains(string(output), png) {
		t.Fatal("saved paths not printed")
	}
}

func TestSuiteReportAndPNG(t *testing.T) {
	a, _ := demo()
	a.Workload = "cpu-sha256-v1"
	a.Operations = 1000
	a.Elapsed = 1
	a.Workers = 1
	b := a
	b.Workload = "gpu-integer-v1"
	b.Operations = 0
	b.GPU = &thermal.GPUResult{Device: "Demo GPU", Backend: "test", Workload: "gpu-integer-v1", Iterations: 2000, Elapsed: 1, Verified: true}
	suite := thermal.NewRun()
	suite.Stage = "baseline"
	suite.Workload = "cpu-gpu-suite-v1"
	suite.Duration = 2
	suite.Elapsed = 2
	suite.Phases = []thermal.Run{a, b}
	dir := t.TempDir()
	path := filepath.Join(dir, "suite.json")
	if _, err := thermal.Save(suite, path); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	png := filepath.Join(dir, "suite.png")
	if run([]string{"report", path, "--png", png}, &stdout, &stderr) != 0 {
		t.Fatal(stderr.String())
	}
	if !strings.Contains(stdout.String(), "hashes/s") || !strings.Contains(stdout.String(), "verified iterations/s") {
		t.Fatal("scores absent from report")
	}
	assertPNG(t, png)
	stdout.Reset()
	stderr.Reset()
	if run([]string{"compare", path, path, "--json", "--png", filepath.Join(dir, "compare.png")}, &stdout, &stderr) != 0 {
		t.Fatal(stderr.String())
	}
	var comparison thermal.Comparison
	if err := json.Unmarshal([]byte(stdout.String()), &comparison); err != nil || len(comparison.Phases) != 2 {
		t.Fatalf("invalid suite comparison: %v", err)
	}
}
