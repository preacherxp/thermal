package thermal

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func channel(ds []Device, id string) *PowerReading {
	for _, d := range ds {
		for _, p := range d.PowerReadings {
			if p.ID == id {
				return &p
			}
		}
	}
	return nil
}
func TestNVIDIADetailedPower(t *testing.T) {
	ds, e := ParseNVIDIA("GPU-1, GPU, 61, 4, 6.3, 210, Not Active, Not Active, 7.2, 6.3, N/A, 160, 80, 5, 175")
	if e != nil {
		t.Fatal(e)
	}
	if p := channel(ds, "power.draw.instant"); p == nil || p.Watts == nil || *p.Watts != 7.2 || p.Mode != "instant" {
		t.Fatalf("%+v", p)
	}
	if p := channel(ds, "power.limit"); p == nil || p.Watts != nil || p.Mode != "limit" {
		t.Fatalf("%+v", p)
	}
	if p := channel(ds, "power.max_limit"); p == nil || *p.Watts != 175 {
		t.Fatalf("%+v", p)
	}
	old, e := ParseNVIDIA("GPU-1, GPU, 61, 4, 6.3, 210, Not Active, Not Active, 100, 95, 80, 5, 175")
	if e != nil {
		t.Fatal(e)
	}
	if channel(old, "power.draw.instant").Watts != nil || *channel(old, "power.limit").Watts != 100 {
		t.Fatal("legacy limit mapping")
	}
}
func TestLHMPackageAndCoreNotSummed(t *testing.T) {
	raw := []byte(`[
 {"Identifier":"/cpu/0/temperature/0","Parent":"/intelcpu/0","Name":"CPU Package","SensorType":"Temperature","Value":65},
 {"Identifier":"/cpu/0/power/0","Parent":"/intelcpu/0","Name":"CPU Package","SensorType":"Power","Value":40},
 {"Identifier":"/cpu/0/power/1","Parent":"/intelcpu/0","Name":"CPU Cores","SensorType":"Power","Value":30},
 {"Identifier":"/gpu/0/power/0","Parent":"/gpu-nvidia/0","Name":"GPU Power","SensorType":"Power","Value":null}
 ]`)
	ds, e := ParseLHM(raw)
	if e != nil {
		t.Fatal(e)
	}
	if len(ds) != 2 || ds[0].Power == nil || *ds[0].Power != 40 || len(ds[0].PowerReadings) != 2 {
		t.Fatalf("%+v", ds)
	}
	if ds[1].Power != nil || ds[1].Temp != nil {
		t.Fatal("unknown power/temperature fabricated")
	}
}
func TestMacmonWattsAndUnknown(t *testing.T) {
	ds, e := ParseMacmon([]byte(`{"temp":{"cpu_temp_avg":43,"gpu_temp_avg":40},"cpu_power":12.5,"gpu_power":4,"gpu_ram_power":0,"sys_power":30}`))
	if e != nil {
		t.Fatal(e)
	}
	if *ds[0].Power != 12.5 || *ds[1].Power != 4 || *channel(ds, "gpu-ram").Watts != 0 {
		t.Fatal("mac power lost")
	}
	if channel(ds, "ram").Watts != nil || channel(ds, "combined").Watts != nil {
		t.Fatal("missing values must not become zero")
	}
}
func TestEnergyCounterWatts(t *testing.T) {
	tests := []struct {
		a, b, max float64
		dt        time.Duration
		want      *float64
	}{
		{1e6, 21e6, 100e6, time.Second, Number(20)},
		{90e6, 10e6, 100e6, time.Second, Number(20)},
		{90e6, 10e6, 0, time.Second, nil},
		{0, 1e6, 100e6, 0, nil},
		{0, 1e6, 100e6, 2 * time.Minute, nil},
	}
	for _, tt := range tests {
		got := energyWatts(tt.a, tt.b, tt.max, tt.dt)
		if tt.want == nil {
			if got != nil {
				t.Fatal(*got)
			}
		} else if got == nil || *got != *tt.want {
			t.Fatalf("%v want %v", got, tt.want)
		}
	}
}
func put(t *testing.T, path, body string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestRAPLWithFixtures(t *testing.T) {
	root := t.TempDir()
	zone := filepath.Join(root, "intel-rapl", "intel-rapl-0")
	put(t, filepath.Join(zone, "name"), "package-0")
	put(t, filepath.Join(zone, "energy_uj"), "1000000")
	put(t, filepath.Join(zone, "max_energy_range_uj"), "100000000")
	put(t, filepath.Join(zone, "constraint_0_name"), "long_term")
	put(t, filepath.Join(zone, "constraint_0_power_limit_uw"), "65000000")
	var r raplSampler
	now := time.Now()
	first := r.read(root, now)
	if len(first) != 1 || first[0].Power != nil {
		t.Fatal("must prime first")
	}
	put(t, filepath.Join(zone, "energy_uj"), "21000000")
	second := r.read(root, now.Add(time.Second))
	if *second[0].Power != 20 || *channel(second, "constraint_0").Watts != 65 {
		t.Fatalf("%+v", second)
	}
	os.Remove(filepath.Join(zone, "energy_uj"))
	lost := r.read(root, now.Add(2*time.Second))
	if lost[0].Power != nil {
		t.Fatal("stale RAPL value reused")
	}
	put(t, filepath.Join(zone, "energy_uj"), "61000000")
	again := r.read(root, now.Add(3*time.Second))
	if again[0].Power != nil {
		t.Fatal("missing counter must re-prime")
	}
}
func TestHwmonMicroWatts(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "hwmon0")
	put(t, filepath.Join(dir, "name"), "amdgpu")
	put(t, filepath.Join(dir, "power1_average"), "25000000")
	put(t, filepath.Join(dir, "power1_input"), "30000000")
	put(t, filepath.Join(dir, "power1_label"), "GPU socket")
	put(t, filepath.Join(dir, "power2_average"), "5000000")
	ds := linuxPower(root)
	if len(ds) != 1 || *ds[0].Power != 25 || len(ds[0].PowerReadings) != 3 {
		t.Fatalf("%+v", ds)
	}
	if *channel(ds, "power2_average").Watts != 5 {
		t.Fatal("subchannel missing")
	}
}
func TestPowerStatisticsAndMissingLastSample(t *testing.T) {
	r := fixture("baseline", 60, 20, 1000)
	r.Samples = r.Samples[:3]
	for i := range r.Samples {
		r.Samples[i].Devices[0].PowerReadings = []PowerReading{power("package", "Package", "draw", Number(float64((i+1)*10))), power("limit", "Limit", "limit", Number(100))}
	}
	r.Samples[2].Devices[0].PowerReadings[0].Watts = nil
	stats := SummarizePower(r, false)["gpu:1"]["package"]
	if *stats.Mean != 15 || *stats.Min != 10 || *stats.Peak != 20 || stats.Current != nil || stats.Samples != 2 {
		t.Fatalf("%+v", stats)
	}
	b := fixture("fans-cleaned", 60, 20, 1000)
	b.Samples = b.Samples[:3]
	for i := range b.Samples {
		b.Samples[i].Devices[0].PowerReadings = []PowerReading{power("package", "Package", "draw", Number(30)), power("limit", "Limit", "limit", Number(100))}
	}
	c := Compare(r, b)
	if len(c.Devices[0].PowerChannels) != 2 {
		t.Fatal("power channels not compared")
	}
	encoded, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	var decoded Run
	if e = json.Unmarshal(encoded, &decoded); e != nil {
		t.Fatal(e)
	}
	if len(decoded.Samples[0].Devices[0].PowerReadings) != 2 {
		t.Fatal("lost JSON power details")
	}
}
func TestPowerOnlyDataDoesNotPassThermalGuard(t *testing.T) {
	ds, e := ParseMacmon([]byte(`{"cpu_power":12}`))
	if e != nil {
		t.Fatal(e)
	}
	if Guard(Sample{Devices: ds}, 90, false) == "" {
		t.Fatal("watts are not temperature")
	}
}
func TestReadableOutputAndNoANSIEscapesInPipes(t *testing.T) {
	r := fixture("baseline", 60, 20, 1000)
	r.Samples[0].Devices[0].Name = "Bad\x1b[2J"
	var out bytes.Buffer
	Report(&out, r)
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("ANSI escape leaked into pipe")
	}
	if !strings.Contains(out.String(), "POWER DRAW (W)") || !strings.Contains(out.String(), "MEAN") {
		t.Fatal(out.String())
	}
	out.Reset()
	PrintSnapshot(&out, Sample{}, []string{"CPU watts unknown"}, "test")
	if !strings.Contains(out.String(), "CPU watts unknown") {
		t.Fatal(out.String())
	}
}
