package thermal

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type Stats struct {
	Name, Kind                            string
	Count                                 int
	Mean, Peak, Util, Power, Clock        *float64
	ThrottleSamples, KnownThrottleSamples int
}

func average(v []float64) *float64 {
	if len(v) == 0 {
		return nil
	}
	n := 0.0
	for _, x := range v {
		n += x
	}
	return Number(n / float64(len(v)))
}
func sustainedCutoff(r Run) float64 {
	if len(r.Samples) == 0 {
		return 0
	}
	first, last := r.Samples[0].Seconds, r.Samples[len(r.Samples)-1].Seconds
	return first + (last-first)*0.75
}
func Summarize(r Run, sustained bool) map[string]Stats {
	type values struct {
		stat                     Stats
		temp, util, power, clock []float64
	}
	all := map[string]*values{}
	cutoff := 0.0
	if sustained && len(r.Samples) > 0 {
		cutoff = sustainedCutoff(r)
	}
	for _, s := range r.Samples {
		if s.Seconds < cutoff {
			continue
		}
		for _, d := range s.Devices {
			v := all[d.ID]
			if v == nil {
				v = &values{stat: Stats{Name: d.Name, Kind: d.Kind}}
				all[d.ID] = v
			}
			if d.Temp != nil {
				v.temp = append(v.temp, *d.Temp)
			}
			if d.Util != nil {
				v.util = append(v.util, *d.Util)
			}
			if d.Power != nil {
				v.power = append(v.power, *d.Power)
			}
			if d.Clock != nil {
				v.clock = append(v.clock, *d.Clock)
			}
			if d.Throttled != nil {
				v.stat.KnownThrottleSamples++
				if *d.Throttled {
					v.stat.ThrottleSamples++
				}
			}
		}
	}
	out := map[string]Stats{}
	for id, v := range all {
		v.stat.Count = len(v.temp)
		v.stat.Mean = average(v.temp)
		v.stat.Util = average(v.util)
		v.stat.Power = average(v.power)
		v.stat.Clock = average(v.clock)
		if len(v.temp) > 0 {
			peak := v.temp[0]
			for _, x := range v.temp {
				peak = math.Max(peak, x)
			}
			v.stat.Peak = Number(peak)
		}
		out[id] = v.stat
	}
	return out
}
func keys(m map[string]Stats) []string {
	k := []string{}
	for id := range m {
		k = append(k, id)
	}
	sort.Strings(k)
	return k
}
func value(v *float64, unit string) string {
	if v == nil {
		return "unknown"
	}
	return fmt.Sprintf("%.1f%s", *v, unit)
}
func throttle(s Stats) string {
	if s.KnownThrottleSamples == 0 {
		return "unknown"
	}
	return fmt.Sprintf("%d/%d samples", s.ThrottleSamples, s.KnownThrottleSamples)
}

// Recommendations are an escalation checklist, never a diagnosis of thermal paste.
func Recommend(r Run) []string {
	out := []string{}
	if len(r.Phases) > 0 {
		for _, phase := range r.Phases {
			for _, note := range Recommend(phase) {
				out = append(out, phase.Workload+": "+note)
			}
		}
		return out
	}
	if r.Status == "refused" {
		if r.Workload == "gpu-integer-v1" {
			return []string{"Check the GPU temperature and monitoring warnings, then repeat when the selected GPU can be monitored."}
		}
		return []string{"Configure CPU temperature monitoring with thermal doctor, then repeat the benchmark."}
	}
	if r.Status == "unavailable" || r.Status == "failed" || r.Status == "skipped" {
		return []string{"This benchmark did not complete. Review its status and warnings before interpreting any readings as benchmark results."}
	}
	high := false
	known := false
	throttled := false
	for _, s := range Summarize(r, true) {
		if s.Kind != "cpu" && s.Kind != "gpu" {
			continue
		}
		if s.Mean != nil {
			known = true
			threshold := 85.0
			if s.Kind == "gpu" {
				threshold = 80
			}
			if *s.Mean >= threshold {
				high = true
			}
		}
		if s.ThrottleSamples > 0 {
			throttled = true
		}
	}
	if !known {
		out = append(out, "Thermal health is unknown: configure a temperature provider with thermal doctor.")
	}
	if r.Status == "stopped" {
		out = append(out, "The benchmark stopped at a monitoring/temperature limit. Let the machine cool before retesting.")
	}
	if len(r.Apps) > 0 {
		out = append(out, "Close unneeded busy apps after saving work; the app list is a brief pre-test CPU sample, not proof of the heat source.")
	}
	if !high && !throttled {
		if known {
			out = append(out, "No elevated sustained temperature detected by the generic screening thresholds. This does not rule out a cooling issue.")
		}
		out = append(out, "Repeat the same workload, duration, power source and room temperature before considering hardware service.")
		return out
	}
	if throttled {
		out = append(out, "The driver reported thermal throttling during the sustained window.")
	}
	out = append(out, "Temperature screening uses 85 C CPU / 80 C GPU, not manufacturer-specific fault limits.")
	switch r.Stage {
	case "baseline":
		out = append(out, "Stage 1 - Close unneeded apps and repeat with --stage apps-closed.",
			"Stage 2 - Try the OS/OEM balanced or cooler thermal profile; record it with --profile and --stage profile-changed. Quieter fan profiles may run hotter.")
	case "apps-closed":
		out = append(out, "Stage 2 - Try an OS/OEM balanced or cooler thermal profile, then repeat with --stage profile-changed and --profile.")
	case "profile-changed":
		out = append(out, "Stage 3 - Check intake clearance, fan operation and dust in vents/fins. Clean according to the device service manual, then repeat with --stage fans-cleaned.")
	case "fans-cleaned":
		out = append(out, "Stage 4 - If repeat tests still show throttling, ask a technician to inspect heatsink contact and thermal interface material. Repasting is a possibility, not established by temperature alone. Liquid metal/shared cooling assemblies favor specialist service.")
	case "repasted":
		out = append(out, "Stage 5 - Persistent trouble after repasting warrants a specialist inspection of fans, mounting/contact, pads and heatpipes.")
	case "specialist":
		out = append(out, "Compare against the original baseline and share these logs with the specialist if the same workload still causes trouble.")
	}
	out = append(out, "Skip directly to a specialist for unexpected shutdowns, a failed fan, or if you are not comfortable opening the device.")
	return out
}

type Comparison struct {
	Phases             []PhaseComparison `json:"phases,omitempty"`
	GPUThroughputDelta *float64          `json:"gpu_iterations_per_second_delta,omitempty"`
	BeforeThroughput   *float64          `json:"before_hashes_per_second,omitempty"`
	AfterThroughput    *float64          `json:"after_hashes_per_second,omitempty"`
	ThroughputDelta    *float64          `json:"hashes_per_second_delta,omitempty"`
	Warnings           []string          `json:"warnings"`
	Devices            []DeviceDelta     `json:"devices"`
}
type PhaseComparison struct {
	Workload   string     `json:"workload"`
	Comparison Comparison `json:"comparison"`
}
type DeviceDelta struct {
	PowerChannels []PowerDelta `json:"power_channels,omitempty"`
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Temperature   *float64     `json:"temperature_delta_c"`
	Clock         *float64     `json:"clock_delta_mhz"`
	Power         *float64     `json:"power_delta_w"`
	Before, After Stats
}

func delta(a, b *float64) *float64 {
	if a == nil || b == nil {
		return nil
	}
	return Number(*b - *a)
}
func Compare(a, b Run) Comparison {
	c := Comparison{Warnings: []string{}, Devices: []DeviceDelta{}}
	if len(a.Phases) > 0 || len(b.Phases) > 0 {
		if len(a.Phases) == 0 || len(b.Phases) == 0 {
			c.Warnings = append(c.Warnings, "Cannot compare a benchmark suite with an individual recording")
			return c
		}
		for _, phase := range a.Phases {
			found := false
			for _, next := range b.Phases {
				if phase.Workload == next.Workload {
					c.Phases = append(c.Phases, PhaseComparison{phase.Workload, Compare(phase, next)})
					found = true
					break
				}
			}
			if !found {
				c.Warnings = append(c.Warnings, "Phase missing after: "+phase.Workload)
			}
		}
		for _, phase := range b.Phases {
			found := false
			for _, prev := range a.Phases {
				if prev.Workload == phase.Workload {
					found = true
					break
				}
			}
			if !found {
				c.Warnings = append(c.Warnings, "New phase after: "+phase.Workload)
			}
		}
		return c
	}
	if a.GPU != nil && b.GPU != nil {
		if a.GPU.Device == b.GPU.Device && a.GPU.Backend == b.GPU.Backend && a.GPU.Workload == b.GPU.Workload {
			c.GPUThroughputDelta = delta(a.GPU.Rate(), b.GPU.Rate())
		} else {
			c.Warnings = append(c.Warnings, "GPU benchmark devices or workloads differ; scores are not comparable")
		}
	}
	if a.Workload == "cpu-sha256-v1" && b.Workload == a.Workload && a.Elapsed > 0 && b.Elapsed > 0 {
		c.BeforeThroughput = Number(float64(a.Operations) / a.Elapsed)
		c.AfterThroughput = Number(float64(b.Operations) / b.Elapsed)
		c.ThroughputDelta = delta(c.BeforeThroughput, c.AfterThroughput)
	}
	meanCPU := func(r Run) *float64 {
		v := []float64{}
		for _, s := range r.Samples {
			if s.CPU != nil {
				v = append(v, *s.CPU)
			}
		}
		return average(v)
	}
	xCPU, yCPU := meanCPU(a), meanCPU(b)
	if xCPU != nil && yCPU != nil && math.Abs(*xCPU-*yCPU) > 10 {
		c.Warnings = append(c.Warnings, "CPU utilization differs by over 10 percentage points")
	}
	pairs := [][3]string{{"host", a.Host, b.Host}, {"OS", a.OS, b.OS}, {"architecture", a.Arch, b.Arch}, {"workload", a.Workload, b.Workload}, {"profile", a.Profile, b.Profile}, {"power source", a.PowerSource, b.PowerSource}}
	for _, p := range pairs {
		if p[1] != p[2] {
			c.Warnings = append(c.Warnings, fmt.Sprintf("%s differs (%s -> %s); this can affect the result", p[0], p[1], p[2]))
		}
	}
	if a.Workers != b.Workers || a.CPUs != b.CPUs {
		c.Warnings = append(c.Warnings, "CPU or worker counts differ")
	}
	if a.Duration != b.Duration || a.Interval != b.Interval {
		c.Warnings = append(c.Warnings, "Recording durations or intervals differ")
	}
	if a.Status != "complete" || b.Status != "complete" {
		c.Warnings = append(c.Warnings, "At least one run is incomplete or stopped early")
	}
	if a.Ambient == nil || b.Ambient == nil {
		c.Warnings = append(c.Warnings, "Ambient temperature is unknown for at least one run")
	} else if math.Abs(*a.Ambient-*b.Ambient) > 2 {
		c.Warnings = append(c.Warnings, "Room temperature differs by more than 2 C")
	}
	if a.Profile == "unknown" || b.Profile == "unknown" || a.PowerSource == "unknown" || b.PowerSource == "unknown" {
		c.Warnings = append(c.Warnings, "Profile or power source is unrecorded")
	}
	for _, run := range []Run{a, b} {
		if len(run.Samples) == 0 {
			continue
		}
		cutoff := sustainedCutoff(run)
		ranges := map[string][]float64{}
		for _, sample := range run.Samples {
			if sample.Seconds < cutoff {
				continue
			}
			for _, d := range sample.Devices {
				if d.Util != nil {
					ranges[d.ID] = append(ranges[d.ID], *d.Util)
				}
			}
		}
		for id, values := range ranges {
			if len(values) < 2 {
				continue
			}
			lo, hi := values[0], values[0]
			for _, v := range values {
				lo = math.Min(lo, v)
				hi = math.Max(hi, v)
			}
			if hi-lo > 20 {
				c.Warnings = append(c.Warnings, "Device utilization varies by over 20 percentage points in "+run.Stage+" ("+id+"); sustained load may not be comparable")
			}
		}
	}
	pa, pb := SummarizePower(a, true), SummarizePower(b, true)
	sa, sb := Summarize(a, true), Summarize(b, true)
	for _, id := range keys(sa) {
		x := sa[id]
		y, ok := sb[id]
		if !ok {
			c.Warnings = append(c.Warnings, "Sensor missing after: "+x.Name)
			continue
		}
		if (x.Mean != nil || y.Mean != nil) && (x.Count < 3 || y.Count < 3) {
			c.Warnings = append(c.Warnings, "Few sustained samples for "+x.Name)
		}
		if x.Util != nil && y.Util != nil && math.Abs(*x.Util-*y.Util) > 10 {
			c.Warnings = append(c.Warnings, "Utilization differs by over 10 percentage points for "+x.Name)
		}

		item := DeviceDelta{ID: id, Name: x.Name, Temperature: delta(x.Mean, y.Mean), Clock: delta(x.Clock, y.Clock), Power: delta(x.Power, y.Power), Before: x, After: y}
		channels := []string{}
		for key := range pa[id] {
			channels = append(channels, key)
		}
		sort.Strings(channels)
		for _, key := range channels {
			first := pa[id][key]
			last, ok := pb[id][key]
			if !ok {
				c.Warnings = append(c.Warnings, "Power channel missing after: "+first.Name)
				continue
			}
			if first.Mode != last.Mode {
				c.Warnings = append(c.Warnings, "Power measurement mode differs: "+first.Name)
				continue
			}
			item.PowerChannels = append(item.PowerChannels, PowerDelta{key, first.Name, first.Mode, first, last, delta(first.Mean, last.Mean), delta(first.Peak, last.Peak)})
		}
		for key, last := range pb[id] {
			if _, ok := pa[id][key]; !ok {
				c.Warnings = append(c.Warnings, "New power channel after: "+last.Name)
			}
		}
		c.Devices = append(c.Devices, item)
	}
	for _, id := range keys(sb) {
		if _, ok := sa[id]; !ok {
			c.Warnings = append(c.Warnings, "New sensor after: "+sb[id].Name)
		}
	}
	if len(c.Devices) == 0 {
		c.Warnings = append(c.Warnings, "No matching sensors; cannot assess thermal changes")
	}
	return c
}
func StageHelp() string { return strings.Join(Stages, ", ") }

// TargetStats selects a deterministic sustained sensor for the requested kind.
func TargetStats(r Run, kind string) (string, Stats) {
	stats := Summarize(r, true)
	if kind == "" && r.Workload == "cpu-sha256-v1" {
		kind = "cpu"
	} else if kind == "" && r.Workload == "gpu-integer-v1" {
		kind = "gpu"
	}
	targetID := ""
	if kind == "gpu" && r.Workload == "gpu-integer-v1" && len(r.Samples) > 0 {
		targetID = r.Samples[0].TargetID
	}
	id := ""
	for _, k := range keys(stats) {
		s := stats[k]
		if kind != "" && s.Kind != kind {
			continue
		}
		if targetID != "" && k != targetID || targetID == "" && kind == "gpu" && r.GPU != nil && !strings.EqualFold(strings.TrimSpace(s.Name), strings.TrimSpace(r.GPU.Device)) {
			continue
		}
		if id == "" || stats[id].Mean == nil && s.Mean != nil || s.Mean != nil && stats[id].Mean != nil && *s.Mean > *stats[id].Mean {
			id = k
		}
	}
	if id == "" {
		return "", Stats{Name: strings.ToUpper(kind), Kind: kind}
	}
	return id, stats[id]
}
