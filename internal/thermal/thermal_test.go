package thermal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sensor(temp float64) Sample {
	return Sample{Devices: []Device{{ID: "cpu:0", Name: "CPU", Kind: "cpu", Temp: Number(temp)}}}
}
func testOptions() Options {
	return Options{Stage: "baseline", Profile: "balanced", PowerSource: "ac", Workload: "idle", Duration: time.Second, Interval: 200 * time.Millisecond, Workers: 1, StopTemp: 90}
}
func TestGuard(t *testing.T) {
	for _, tt := range []struct {
		name  string
		s     Sample
		allow bool
		want  string
	}{
		{"cool", sensor(60), false, ""},
		{"hot", sensor(90), false, "temperature limit"},
		{"lost", Sample{}, false, "unavailable"},
		{"explicit bypass", Sample{}, true, ""},
		{"gpu is not cpu", Sample{Devices: []Device{{Kind: "gpu", Temp: Number(50)}}}, false, "unavailable"},
		{"bypass still stops heat", sensor(95), true, "temperature limit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := Guard(tt.s, 90, tt.allow)
			if tt.want == "" && got != "" || tt.want != "" && !strings.Contains(got, tt.want) {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
	s := sensor(81)
	s.Devices[0].Critical = Number(85)
	if Guard(s, 90, false) == "" {
		t.Fatal("critical margin not honored")
	}
}
func TestNVIDIAParsing(t *testing.T) {
	ds, err := ParseNVIDIA("GPU-1, Example GPU, 87, 100, 104.5, 1882, Active, Not Active\nGPU-2, Other GPU, N/A, 0, [Not Supported], 210, N/A, N/A\n")
	if err != nil || len(ds) != 2 {
		t.Fatalf("%v %v", ds, err)
	}
	if ds[0].Throttled == nil || !*ds[0].Throttled || *ds[0].Power != 104.5 {
		t.Fatal("lost GPU data")
	}
	if ds[1].Temp != nil || ds[1].Power != nil || ds[1].Throttled != nil {
		t.Fatal("missing metrics must stay unknown")
	}
	if _, err = ParseNVIDIA("broken"); err == nil {
		t.Fatal("accepted malformed row")
	}
	ds, err = ParseNVIDIA("GPU-1, Example, 50, 0, 10, 210")
	if err != nil || ds[0].Throttled != nil {
		t.Fatal("basic-query fallback broken")
	}
}
func fixture(stage string, temp, power, clock float64) Run {
	r := NewRun()
	r.Host = "fixture"
	r.Stage = stage
	r.Profile = "performance"
	r.PowerSource = "ac"
	r.Workload = "gpu-test"
	r.Duration = 120
	r.Interval = 5
	r.Ambient = Number(22)
	for i := 0; i <= 24; i++ {
		v := true
		r.Samples = append(r.Samples, Sample{Seconds: float64(i * 5), Devices: []Device{{ID: "gpu:1", Name: "GPU", Kind: "gpu", Temp: Number(temp), Power: Number(power), Clock: Number(clock), Util: Number(100), Throttled: &v}}})
	}
	return r
}
func TestSameCeilingPerformanceComparison(t *testing.T) {
	a, b := fixture("baseline", 87, 78, 1563), fixture("fans-cleaned", 87, 104, 1882)
	c := Compare(a, b)
	if len(c.Warnings) != 0 || len(c.Devices) != 1 {
		t.Fatalf("%+v", c)
	}
	d := c.Devices[0]
	if *d.Temperature != 0 || *d.Power != 26 || *d.Clock != 319 {
		t.Fatalf("%+v", d)
	}
	next := strings.Join(Recommend(b), " ")
	if !strings.Contains(next, "technician") || !strings.Contains(next, "not established") {
		t.Fatal(next)
	}
}
func TestComparisonMismatchesAndMissingData(t *testing.T) {
	a, b := fixture("baseline", 87, 78, 1563), fixture("fans-cleaned", 87, 104, 1882)
	b.Workload = "different"
	b.Profile = "quiet"
	b.Status = "stopped"
	b.Ambient = nil
	b.Samples[24].Devices[0].Temp = nil
	if len(Compare(a, b).Warnings) < 4 {
		t.Fatal("missing comparison warnings")
	}
	for i := range b.Samples {
		b.Samples[i].Devices[0].ID = "gpu:2"
	}
	if len(Compare(a, b).Devices) != 0 {
		t.Fatal("unmatched hardware compared")
	}
}
func TestSustainedWindow(t *testing.T) {
	r := fixture("baseline", 70, 80, 1000)
	r.Samples[0].Devices[0].Temp = Number(120)
	if *Summarize(r, true)["gpu:1"].Peak != 70 {
		t.Fatal("included startup peak in sustained window")
	}
	if *Summarize(r, false)["gpu:1"].Peak != 120 {
		t.Fatal("lost full-run peak")
	}
}
func TestStageRecommendations(t *testing.T) {
	words := map[string]string{"baseline": "Close", "apps-closed": "profile", "profile-changed": "dust", "fans-cleaned": "technician", "repasted": "specialist", "specialist": "logs"}
	for stage, word := range words {
		if !strings.Contains(strings.Join(Recommend(fixture(stage, 87, 80, 1000)), " "), word) {
			t.Fatal(stage)
		}
	}
	r := fixture("fans-cleaned", 50, 80, 1000)
	for i := range r.Samples {
		r.Samples[i].Devices[0].Temp = nil
		r.Samples[i].Devices[0].Throttled = nil
	}
	if !strings.Contains(strings.Join(Recommend(r), " "), "unknown") {
		t.Fatal("unknown temperature treated as healthy")
	}
}
func TestStorage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.json")
	r := fixture("baseline", 70, 80, 1000)
	if _, err := Save(r, path); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(r, path); err == nil {
		t.Fatal("overwrote run")
	}
	got, err := Load(path)
	if err != nil || len(got.Samples) != 25 {
		t.Fatalf("%+v %v", got, err)
	}
	invalid := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(invalid, []byte("{\"schema\":2}"), 0600)
	if _, err = Load(invalid); err == nil {
		t.Fatal("accepted unsupported schema")
	}
}

type fakeReader struct {
	samples []Sample
	index   int
}

func (f *fakeReader) Read(context.Context) (Sample, []string) {
	i := f.index
	f.index++
	if i >= len(f.samples) {
		i = len(f.samples) - 1
	}
	return f.samples[i], nil
}
func TestBenchmarkRefusesWithoutCPUSensor(t *testing.T) {
	o := testOptions()
	o.Benchmark = true
	r, e := Capture(context.Background(), &fakeReader{samples: []Sample{{}}}, o, nil)
	if e != nil || r.Status != "refused" || r.Operations != 0 {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestBenchmarkStopsOnSensorLoss(t *testing.T) {
	o := testOptions()
	o.Benchmark = true
	reader := &fakeReader{samples: []Sample{sensor(50), sensor(50), {}}}
	r, e := Capture(context.Background(), reader, o, nil)
	if e != nil || r.Status != "stopped" || r.Elapsed > 2 {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestBenchmarkTimeBound(t *testing.T) {
	o := testOptions()
	o.Benchmark = true
	r, e := Capture(context.Background(), &fakeReader{samples: []Sample{sensor(50)}}, o, nil)
	if e != nil || r.Status != "complete" || r.Elapsed > 2 || r.Operations == 0 {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestCancelledCapture(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, e := Capture(ctx, &fakeReader{samples: []Sample{sensor(50)}}, testOptions(), nil)
	if e != nil || r.Status != "interrupted" {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestThroughputAndVariableLoad(t *testing.T) {
	a, b := fixture("baseline", 87, 78, 1563), fixture("fans-cleaned", 87, 104, 1882)
	a.Workload = "cpu-sha256-v1"
	b.Workload = a.Workload
	a.Operations = 1000
	a.Elapsed = 10
	b.Operations = 1500
	b.Elapsed = 10
	b.Samples[24].Devices[0].Util = Number(50)
	c := Compare(a, b)
	if c.ThroughputDelta == nil || *c.ThroughputDelta != 50 {
		t.Fatal("throughput delta missing")
	}
	if !strings.Contains(strings.Join(c.Warnings, " "), "varies by over 20") {
		t.Fatal("transient load drop not flagged")
	}
}

func TestSustainedWindowWithOffsetTimestamps(t *testing.T) {
	r := fixture("baseline", 60, 20, 1000)
	r.Samples = nil
	for i := 0; i < 5; i++ {
		r.Samples = append(r.Samples, Sample{Seconds: 100 + float64(i)*5, Devices: []Device{{ID: "gpu", Kind: "gpu", Temp: Number(float64(i) * 20), Power: Number(float64(i) * 20), Util: Number(float64(i) * 10)}}})
	}
	temp := Summarize(r, true)["gpu"].Mean
	power := SummarizePower(r, true)["gpu"]["primary"].Mean
	if temp == nil || power == nil || *temp != 70 || *power != 70 {
		t.Fatalf("incorrect sustained means: temp=%v power=%v", temp, power)
	}
	for _, warning := range Compare(r, r).Warnings {
		if strings.Contains(warning, "varies by over 20") {
			t.Fatal("comparison included utilization outside the sustained window")
		}
	}
}
