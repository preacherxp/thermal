package thermal

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gpuSample(temp float64) Sample {
	return Sample{Devices: []Device{{ID: "gpu:0", Name: "Test GPU", Kind: "gpu", Temp: Number(temp)}}}
}

type fakeGPUSession struct {
	started, closed bool
	failure         error
}

func (g *fakeGPUSession) Result() (GPUResult, error, bool) {
	r := GPUResult{Device: "Test GPU", Backend: "test", Workload: "gpu-integer-v1"}
	if g.started {
		r.Iterations = 1000
		r.Elapsed = 1
		r.Verified = true
	}
	return r, g.failure, g.closed
}
func (g *fakeGPUSession) Start() error { g.started = true; return nil }
func (g *fakeGPUSession) Close()       { g.closed = true }

func TestGPUCaptureMonitoring(t *testing.T) {
	for _, tt := range []struct {
		name    string
		samples []Sample
		allow   bool
		status  string
		started bool
	}{
		{"missing", []Sample{{}}, false, "refused", false},
		{"hot", []Sample{gpuSample(95)}, false, "refused", false},
		{"unrelated sensor", []Sample{sensor(50)}, false, "refused", false},
		{"sensor loss", []Sample{gpuSample(50), gpuSample(50), gpuSample(50), {}}, false, "stopped", true},
		{"overheat", []Sample{gpuSample(50), gpuSample(50), gpuSample(50), gpuSample(95)}, false, "stopped", true},
		{"heated during setup", []Sample{gpuSample(50), gpuSample(50), gpuSample(95)}, false, "refused", false},
		{"complete", []Sample{gpuSample(50)}, false, "complete", true},
		{"explicit unmonitored", []Sample{{}}, true, "complete", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o := testOptions()
			o.AllowUnmonitored = tt.allow
			g := &fakeGPUSession{}
			r, err := captureGPU(context.Background(), &fakeReader{samples: tt.samples}, o, nil, func(context.Context, time.Duration) (gpuSession, error) { return g, nil })
			if err != nil || r.Status != tt.status || g.started != tt.started || !g.closed {
				t.Fatalf("status=%s started=%v closed=%v err=%v", r.Status, g.started, g.closed, err)
			}
			if !tt.started && r.GPU.Rate() != nil {
				t.Fatal("refused benchmark has a score")
			}
		})
	}
}

func TestGPUUnavailableAndFailed(t *testing.T) {
	o := testOptions()
	r, err := captureGPU(context.Background(), &fakeReader{samples: []Sample{gpuSample(50)}}, o, nil, func(context.Context, time.Duration) (gpuSession, error) { return nil, errors.New("no GPU driver") })
	if err != nil || r.Status != "unavailable" || r.GPU != nil || !strings.Contains(strings.Join(r.Warnings, " "), "no GPU driver") {
		t.Fatalf("%+v %v", r, err)
	}
	g := &fakeGPUSession{failure: errors.New("device lost")}
	r, err = captureGPU(context.Background(), &fakeReader{samples: []Sample{gpuSample(50)}}, o, nil, func(context.Context, time.Duration) (gpuSession, error) { return g, nil })
	if err != nil || r.Status != "failed" || !g.closed {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestGPUCancellationDoesNotLaunchWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := captureGPU(ctx, &fakeReader{samples: []Sample{gpuSample(50)}}, testOptions(), nil, func(context.Context, time.Duration) (gpuSession, error) {
		t.Fatal("started after cancellation")
		return nil, nil
	})
	if err != nil || r.Status != "interrupted" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestGPUSensorIdentityAndCriticalLimit(t *testing.T) {
	s := gpuSample(81)
	s.Devices[0].Critical = Number(85)
	if gpuGuard(s, "gpu:0", 90, true) == "" {
		t.Fatal("critical margin ignored")
	}
	if gpuSensorID(s, "Different GPU") != "" {
		t.Fatal("unrelated sensor selected")
	}
	s.Devices = append(s.Devices, s.Devices[0])
	s.Devices[1].ID = "gpu:1"
	if gpuSensorID(s, "Test GPU") != "" {
		t.Fatal("ambiguous GPU sensor selected")
	}
}

func TestBenchmarkSuitePartialAndThermalStop(t *testing.T) {
	for _, status := range []string{"complete", "refused", "stopped", "interrupted"} {
		t.Run(status, func(t *testing.T) {
			o := testOptions()
			gpuCalls := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cpu := func(ctx context.Context, _ Reader, o Options, _ func(Sample)) (Run, error) {
				r := benchmarkRun(o, "cpu-sha256-v1")
				r.Status = status
				r.Samples = []Sample{sensor(50)}
				r.Operations = 1000
				r.Elapsed = 1
				if status == "interrupted" {
					cancel()
				}
				return r, nil
			}
			gpu := func(_ context.Context, _ Reader, o Options, _ func(Sample)) (Run, error) {
				gpuCalls++
				r := benchmarkRun(o, "gpu-integer-v1")
				r.Samples = []Sample{gpuSample(50)}
				return r, nil
			}
			r, err := captureBenchmarks(ctx, nil, o, "both", nil, cpu, gpu)
			if err != nil || len(r.Phases) != 2 || len(r.Samples) != 0 || r.Duration != 2 {
				t.Fatalf("%+v %v", r, err)
			}
			if status == "stopped" || status == "interrupted" {
				if gpuCalls != 0 || r.Phases[1].Status != "skipped" {
					t.Fatal("loaded GPU after CPU stop")
				}
			} else if gpuCalls != 1 {
				t.Fatal("did not attempt independent GPU phase")
			}
			if (r.Status == "complete") != (status == "complete") {
				t.Fatalf("incorrect suite status %s", r.Status)
			}
			path := filepath.Join(t.TempDir(), "suite.json")
			if _, err := Save(r, path); err != nil {
				t.Fatal(err)
			}
			loaded, err := Load(path)
			if err != nil || len(loaded.Phases) != 2 {
				t.Fatalf("cannot reload suite: %v", err)
			}
		})
	}
}

func TestSuiteComparisonKeepsPhasesSeparate(t *testing.T) {
	a, b := benchmarkRun(testOptions(), "cpu-gpu-suite-v1"), benchmarkRun(testOptions(), "cpu-gpu-suite-v1")
	cpu := fixture("baseline", 60, 20, 1000)
	cpu.Workload = "cpu-sha256-v1"
	cpu.Operations = 1000
	cpu.Elapsed = 1
	gpu := fixture("baseline", 70, 100, 2000)
	gpu.Workload = "gpu-integer-v1"
	gpu.GPU = &GPUResult{Device: "Test GPU", Backend: "test", Workload: "gpu-integer-v1", Iterations: 1000, Elapsed: 1, Verified: true}
	a.Phases = []Run{cpu, gpu}
	cpu.Operations = 1500
	gpu.GPU = &GPUResult{Device: "Test GPU", Backend: "test", Workload: "gpu-integer-v1", Iterations: 2000, Elapsed: 1, Verified: true}
	b.Phases = []Run{cpu, gpu}
	c := Compare(a, b)
	if len(c.Phases) != 2 || c.Phases[0].Comparison.ThroughputDelta == nil || *c.Phases[0].Comparison.ThroughputDelta != 500 || c.Phases[1].Comparison.GPUThroughputDelta == nil || *c.Phases[1].Comparison.GPUThroughputDelta != 1000 {
		t.Fatalf("%+v", c)
	}
	if len(Compare(a, cpu).Warnings) == 0 {
		t.Fatal("suite compared with individual run")
	}
	b.Phases[1].GPU.Device = "Other GPU"
	if Compare(a, b).Phases[1].Comparison.GPUThroughputDelta != nil {
		t.Fatal("different GPUs compared")
	}
}

func TestGPUProgressCarriesGuardSensor(t *testing.T) {
	g := &fakeGPUSession{}
	s := gpuSample(50)
	s.Devices = append([]Device{{ID: "other", Name: "Idle GPU", Kind: "gpu", Temp: Number(40)}}, s.Devices...)
	var progress []Sample
	r, err := captureGPU(context.Background(), &fakeReader{samples: []Sample{s}}, testOptions(), func(s Sample) { progress = append(progress, s) }, func(context.Context, time.Duration) (gpuSession, error) { return g, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(progress) < 3 || progress[0].TargetID != "" {
		t.Fatal("expected discovery then selected-target progress")
	}
	for _, sample := range progress[1:] {
		if sample.TargetID != "gpu:0" {
			t.Fatalf("wrong progress sensor: %q", sample.TargetID)
		}
	}
	path, err := Save(r, filepath.Join(t.TempDir(), "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := TargetStats(restored, "gpu"); id != "gpu:0" {
		t.Fatalf("saved target changed: %q", id)
	}
}
