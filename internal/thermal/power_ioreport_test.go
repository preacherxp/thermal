package thermal

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestIOReportEnergyAndMissingReadings(t *testing.T) {
	for _, tc := range []struct {
		unit  string
		delta int64
	}{{"mJ", 2500}, {"uJ", 2500000}, {"nJ", 2500000000}} {
		before := ioEnergy{"CPU Energy", tc.unit, 1 << 60}
		after := before
		after.value += tc.delta
		got := ioEnergyWatts(before, after, 500*time.Millisecond)
		if got == nil || *got != 5 {
			t.Fatalf("%s: %v", tc.unit, got)
		}
	}
	for _, tc := range []struct {
		before, after ioEnergy
		dt            time.Duration
	}{
		{ioEnergy{unit: "uJ", value: 10}, ioEnergy{unit: "uJ", value: 9}, time.Second},
		{ioEnergy{unit: "uJ", value: -1}, ioEnergy{unit: "uJ", value: 10}, time.Second},
		{ioEnergy{unit: "uJ"}, ioEnergy{unit: "mJ", value: 10}, time.Second},
		{ioEnergy{unit: "W"}, ioEnergy{unit: "W", value: 10}, time.Second},
		{ioEnergy{unit: "uJ"}, ioEnergy{unit: "uJ", value: 10}, 0},
		{ioEnergy{unit: "uJ"}, ioEnergy{unit: "uJ", value: 10}, 2 * time.Minute},
	} {
		if ioEnergyWatts(tc.before, tc.after, tc.dt) != nil {
			t.Fatalf("accepted invalid counter: %+v", tc)
		}
	}
	at := time.Now()
	r := &ioReportSampler{}
	samples := []ioEnergy{{"CPU Energy", "mJ", 1000}, {"GPU Energy", "mJ", 1000}}
	first := r.update(samples, at)
	if first["cpu"][0].Watts != nil || first["gpu"][0].Watts != nil {
		t.Fatal("first sample fabricated watts")
	}
	r.update(samples, at.Add(time.Millisecond))
	samples[0].value += 2000
	result := r.update(samples, at.Add(time.Second))
	if *result["cpu"][0].Watts != 2 || *result["gpu"][0].Watts != 0 {
		t.Fatal("priming lost baseline or zero GPU draw")
	}
	if r.update(nil, at.Add(2*time.Second))["cpu"][0].Watts != nil {
		t.Fatal("missing counter was retained")
	}
	if r.update(samples, at.Add(3*time.Second))["cpu"][0].Watts != nil {
		t.Fatal("reappearing counter needs a new baseline")
	}
}

func TestIOReportDomainsAndReports(t *testing.T) {
	at := time.Now()
	for _, aggregate := range []bool{false, true} {
		r := &ioReportSampler{}
		channels := []ioEnergy{{"DIE_0_CPU Energy", "mJ", 1000}, {"DIE_1_CPU Energy", "mJ", 1000}, {"GPU Energy", "mJ", 1000}, {"GPU SRAM Energy", "mJ", 1000}, {"PCPU Energy", "mJ", 1000}}
		if aggregate {
			channels = append(channels, ioEnergy{"CPU Energy", "mJ", 1000})
		}
		r.update(channels, at)
		for i := range channels {
			channels[i].value += 2000
		}
		devices := []Device{{ID: "smc:TCMz", Name: "CPU die", Kind: "cpu", Source: "AppleSMC", Temp: Number(55)}, {ID: "smc:gpu", Name: "Apple M4 Pro", Kind: "gpu", Source: "AppleSMC", Temp: Number(50)}}
		devices = attachIOReportPower(devices, r.update(channels, at.Add(time.Second)))
		want := 4.0
		if aggregate {
			want = 2
		}
		if len(devices) != 2 || *devices[0].Power != want || *devices[1].Power != 2 || *devices[0].Temp != 55 || gpuSensorID(Sample{Devices: devices}, "Apple M4 Pro") != "smc:gpu" {
			t.Fatalf("incorrect domain totals or sensor identity: %+v", devices)
		}
		run := NewRun()
		run.Samples = []Sample{{Devices: devices}}
		var report bytes.Buffer
		Report(&report, run)
		if !strings.Contains(report.String(), "IOReport estimate") || len(PowerWarnings(devices)) != 0 {
			t.Fatal("power missing from report")
		}
		encoded, err := json.Marshal(run)
		if err != nil || !strings.Contains(string(encoded), `"power_w":`) || !strings.Contains(string(encoded), `"mode":"energy-average"`) {
			t.Fatal("power missing from saved JSON")
		}
		if !aggregate {
			// Even repeated partial samples must not silently lower the CPU total.
			for i := 2; i < 4; i++ {
				readings := r.update(channels[:1], at.Add(time.Duration(i)*time.Second))
				ds := attachIOReportPower(nil, readings)
				if ds[0].Power != nil || ds[0].Temp != nil {
					t.Fatal("partial die set or power-only device fabricated readings")
				}
			}
		}
	}
	for _, name := range []string{"CPU Energy Budget", "GPU SRAM Energy", "DIE_bad_CPU Energy", "DIE_-1_CPU Energy", "DIE_0_DIE_1_CPU Energy"} {
		if ioEnergyKind(name) != "" {
			t.Fatalf("unrecognized domain accepted: %s", name)
		}
	}
}
