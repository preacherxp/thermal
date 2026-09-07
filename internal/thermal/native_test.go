package thermal

import (
	"math"
	"os"
	"strings"
	"testing"
)

func TestCPUCounterIntervals(t *testing.T) {
	var s cpuSampler
	if s.update(100, 30) != nil {
		t.Fatal("first reading must be unknown")
	}
	if v := s.update(200, 50); v == nil || *v != 80 {
		t.Fatalf("bad interval: %v", v)
	}
	if s.update(200, 50) != nil {
		t.Fatal("zero interval must be unknown")
	}
	if s.update(10, 3) != nil {
		t.Fatal("reset must be unknown")
	}
	if v := s.update(110, 23); v == nil || *v != 80 {
		t.Fatal("must recover after reset")
	}
	if s.update(120, 50) != nil {
		t.Fatal("idle delta exceeds total")
	}
}
func TestNativeConsoleAndProviderDefaults(t *testing.T) {
	t.Setenv("THERMAL_EXTERNAL_PROVIDERS", "")
	if externalProviders() {
		t.Fatal("external tools must be opt in")
	}
	t.Setenv("THERMAL_EXTERNAL_PROVIDERS", "1")
	if !externalProviders() {
		t.Fatal("opt in ignored")
	}
	if TerminalFile(nil) {
		t.Fatal("nil is not a terminal")
	}
	f, e := os.Open(os.DevNull)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if TerminalFile(f) {
		t.Fatal("null device is not a terminal")
	}
}
func TestProcParsers(t *testing.T) {
	total, idle, ok := parseCPUTicks("cpu 100 10 30 200 20 4 6 8 100 10\ncpu0 1 2 3 4")
	if !ok || total != 378 || idle != 220 {
		t.Fatalf("guest time double-counted: %d %d", total, idle)
	}
	if _, _, ok := parseCPUTicks("cpu 1 bad 3 4"); ok {
		t.Fatal("bad counters accepted")
	}
	fields := strings.Fields("S 1 2 3 4 5 6 7 8 9 10 120 30 0 0 0 0 0 0 98765")
	p, ok := parseProcessStat("42 (game (worker)) "+strings.Join(fields, " "), 100)
	if !ok || p.pid != 42 || p.name != "game (worker)" || p.seconds != 1.5 || p.start != "98765" {
		t.Fatalf("%+v %v", p, ok)
	}
	for _, raw := range []string{"", "42 (broken)", "42 (x) " + strings.Repeat("bad ", 20)} {
		if _, ok := parseProcessStat(raw, 100); ok {
			t.Fatal("malformed stat accepted")
		}
	}
}
func TestMacOSTextCounters(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want float64
	}{{"01:02.50", 62.5}, {"02:03:04", 7384}, {"1-02:03:04", 93784}} {
		got, ok := parseProcessTime(tc.in)
		if !ok || math.Abs(got-tc.want) > 0.001 {
			t.Fatalf("%s: %v", tc.in, got)
		}
	}
	for _, v := range []string{"bad", "NaN:00", "-1:30"} {
		if _, ok := parseProcessTime(v); ok {
			t.Fatalf("accepted %q", v)
		}
	}
	got := parseMacCPU("CPU usage: 90.0% user, 1.0% sys, 9.0% idle\nCPU usage: 3.25% user, 2.5% sys, 94.25% idle")
	if got == nil || *got != 5.75 {
		t.Fatalf("must use final interval: %v", got)
	}
	if parseMacCPU("CPU usage: bad% user, 0% sys, 100% idle") != nil {
		t.Fatal("malformed sample")
	}
}
