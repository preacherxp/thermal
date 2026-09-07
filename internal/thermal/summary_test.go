package thermal

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBenchmarkSummaryKeepsTargetsAndStatusSeparate(t *testing.T) {
	cpu := fixture("baseline", 60, 20, 1000)
	cpu.Workload, cpu.Status = "cpu-sha256-v1", "refused"
	cpu.Warnings = []string{"CPU wattage unavailable", "CPU temperature unavailable; configure sensors"}
	gpu := fixture("baseline", 87, 100, 1800)
	gpu.Workload, gpu.Elapsed = "gpu-integer-v1", 30
	gpu.GPU = &GPUResult{Device: "GPU", Backend: "test", Iterations: 51833634284055, Elapsed: 30, Verified: true}
	suite := NewRun()
	suite.Status, suite.Stage, suite.Phases = "partial", "baseline", []Run{cpu, gpu}
	var out bytes.Buffer
	BenchmarkSummary(&out, suite)
	text := out.String()
	for _, want := range []string{"BENCHMARK / PARTIAL", "CPU / REFUSED", "CPU temperature unavailable", "GPU / COMPLETE", "1.73 T verified iterations/s", "87.0 C", "Throttling", "--verbose"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	cpuText := strings.Split(strings.Split(text, "CPU / REFUSED")[1], "GPU / COMPLETE")[0]
	if strings.Contains(cpuText, "60.0 C") || strings.Contains(cpuText, "Score") {
		t.Fatal("refused CPU inherited GPU measurements")
	}
	for _, unwanted := range []string{"\x1b", "POWER LIMITS", "BUSY APPS", "cpu-sha256-v1", "1727787809468"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("noisy summary contains %q", unwanted)
		}
	}
	if strings.Count(text, "\n") > 28 {
		t.Fatal("summary too long")
	}
	cpu.Status, cpu.Elapsed, cpu.Operations = "complete", 1, 1000
	cpu.Warnings = nil
	out.Reset()
	BenchmarkSummary(&out, cpu)
	if !strings.Contains(out.String(), "unknown mean / unknown peak") || !strings.Contains(out.String(), "Configure target temperature") {
		t.Fatalf("missing CPU temperature was hidden: %s", &out)
	}
}

func TestPlainProgressIsSparseAndResetsForEachPhase(t *testing.T) {
	var out bytes.Buffer
	update, finish := NewProgress(&out, 10)
	for phase := 0; phase < 2; phase++ {
		for second := 0; second <= 10; second++ {
			update(Sample{Seconds: float64(second)})
		}
		finish()
	}
	if strings.Count(out.String(), "\n") != 6 || strings.Contains(out.String(), "\x1b") {
		t.Fatalf("expected first, five-second, final updates per phase:\n%s", &out)
	}
}

func TestColorDisabledForFilesAndEnvironment(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "console.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if terminalColor(f) {
		t.Fatal("regular file supports ANSI")
	}
	for _, env := range []string{"NO_COLOR", "TERM"} {
		t.Run(env, func(t *testing.T) {
			if env == "TERM" {
				t.Setenv(env, "dumb")
			} else {
				t.Setenv(env, "")
			}
			if newDisplay(os.Stdout).color {
				t.Fatal("environment did not disable ANSI")
			}
		})
	}
}
