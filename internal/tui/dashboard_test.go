package tui

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"thermal-cli/internal/thermal"
)

func fixtureModel(t *testing.T) *model {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := newModel(ctx, cancel, Config{Mode: "benchmark", Target: "both", Duration: 30, StopTemp: 90}, io.Discard, nil)
	m.width, m.height = 104, 38
	yes := true
	for second := 0; second <= 20; second++ {
		m.sample(sampleMsg{"GPU", thermal.Sample{TargetID: "gpu", Seconds: float64(second), Devices: []thermal.Device{{ID: "gpu", Name: "NVIDIA GeForce RTX 4080 Laptop GPU", Kind: "gpu", Temp: thermal.Number(60 + float64(second)), Power: thermal.Number(137.5), Clock: thermal.Number(2423), Throttled: &yes}}}})
	}
	m.cards[0].state = "Awaiting result"
	return m
}
func TestDashboardResponsiveAndSafeText(t *testing.T) {
	m := fixtureModel(t)
	for _, size := range [][2]int{{104, 38}, {80, 24}, {40, 20}, {24, 10}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View()
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("%v: line wider than terminal: %d", size, ansi.StringWidth(line))
			}
		}
		if size[1] < 28 && len(strings.Split(view, "\n")) > size[1] {
			t.Fatalf("%v: dashboard taller than terminal", size)
		}
		if !strings.Contains(ansi.Strip(view), "GPU") {
			t.Fatal("GPU card missing")
		}
	}
	m.width, m.height = 104, 38
	if dir := os.Getenv("THERMAL_TUI_PREVIEW"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, "tui-preview.ansi"), []byte(m.View()), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.cards[1].device = "bad\x1b[2Jdevice"
	if strings.Contains(m.View(), "\x1b[2J") {
		t.Fatal("device name injected terminal controls")
	}
}
func TestDashboardFinalResultPreservesUnknownAndRefused(t *testing.T) {
	m := fixtureModel(t)
	cpu := thermal.NewRun()
	cpu.Workload, cpu.Status, cpu.Warnings = "cpu-sha256-v1", "refused", []string{"CPU temperature unavailable"}
	gpu := thermal.NewRun()
	gpu.Workload, gpu.Elapsed = "gpu-integer-v1", 30
	gpu.GPU = &thermal.GPUResult{Device: "Target GPU", Iterations: 51000000000000, Elapsed: 30, Verified: true}
	gpu.Samples = []thermal.Sample{{Seconds: 30, Devices: []thermal.Device{{ID: "other", Kind: "gpu", Name: "Other GPU", Temp: thermal.Number(90)}}}}
	r := thermal.NewRun()
	r.Status, r.Phases = "partial", []thermal.Run{cpu, gpu}
	m.finish(Result{Run: r, Saved: "Saved run.pdf\nSaved run.png\nSaved run.json\n"})
	view := ansi.Strip(m.render())
	for _, want := range []string{"refused", "CPU temperature unavailable", "1.70 T iterations/s", "Unknown", "run.pdf"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in %s", want, view)
		}
	}
	if m.cards[1].temp != nil || m.cards[0].score != "" {
		t.Fatal("inherited unrelated device readings")
	}
}
func TestDashboardCancellationWaitsForSavedResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	m := newModel(ctx, cancel, Config{Mode: "benchmark", Target: "cpu", Duration: 30}, io.Discard, func(ctx context.Context, _ func(string, thermal.Sample)) Result {
		close(started)
		<-ctx.Done()
		return Result{Run: thermal.Run{Status: "interrupted"}, Saved: "Saved partial.json"}
	})
	p := tea.NewProgram(m, tea.WithInput(strings.NewReader("")), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	finished := make(chan error, 1)
	go func() { _, err := p.Run(); finished <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		p.Kill()
		t.Fatal("work did not start")
	}
	p.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		p.Kill()
		t.Fatal("cancellation failed to finish")
	}
	if m.result == nil || m.result.Saved != "Saved partial.json" {
		t.Fatal("quit before reports were saved")
	}
}
func TestPipesAndDisabledColorUseFallback(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "redirected.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if Available(os.Stdin, f, os.Stderr) || Available(os.Stdin, &bytes.Buffer{}, os.Stderr) {
		t.Fatal("redirected output enabled TUI")
	}
	t.Setenv("NO_COLOR", "1")
	if Available(os.Stdin, os.Stdout, os.Stderr) {
		t.Fatal("NO_COLOR did not select fallback")
	}
}

func TestLiveGPUUsesSelectedSensor(t *testing.T) {
	m := fixtureModel(t)
	sample := thermal.Sample{TargetID: "b", Devices: []thermal.Device{
		{ID: "a", Name: "Idle GPU", Kind: "gpu", Temp: thermal.Number(40)},
		{ID: "b", Name: "Benchmark GPU", Kind: "gpu", Temp: thermal.Number(89)},
	}}
	m.sample(sampleMsg{phase: "GPU", sample: sample})
	if m.cards[1].device != "Benchmark GPU" || m.cards[1].temp == nil || *m.cards[1].temp != 89 {
		t.Fatalf("wrong live GPU: %+v", m.cards[1])
	}
	sample.Devices = sample.Devices[:1]
	m.sample(sampleMsg{phase: "GPU", sample: sample})
	if m.cards[1].temp != nil {
		t.Fatal("lost sensor replaced by unrelated GPU")
	}
	sample.TargetID = ""
	m.sample(sampleMsg{phase: "GPU", sample: sample})
	if m.cards[1].temp != nil {
		t.Fatal("GPU selected before discovery")
	}
}
