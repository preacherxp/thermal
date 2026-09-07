package thermal

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

type ioEnergy struct {
	name, unit string
	value      int64
}
type ioEnergyPoint struct {
	ioEnergy
	at time.Time
}
type ioReportSampler struct {
	previous map[string]ioEnergyPoint
	names    map[string]bool
}

func ioEnergyKind(name string) string {
	if strings.HasPrefix(name, "DIE_") {
		die, rest, ok := strings.Cut(strings.TrimPrefix(name, "DIE_"), "_")
		n, err := strconv.Atoi(die)
		if !ok || err != nil || n < 0 || strconv.Itoa(n) != die {
			return ""
		}
		name = rest
	}
	switch name {
	case "CPU Energy":
		return "cpu"
	case "GPU Energy":
		return "gpu"
	}
	return ""
}

func ioEnergyWatts(before, after ioEnergy, elapsed time.Duration) *float64 {
	if before.unit != after.unit || before.value < 0 || after.value < before.value {
		return nil
	}
	// Subtract integers before conversion, preserving small deltas in large counters.
	microjoules := float64(after.value - before.value)
	switch after.unit {
	case "mJ":
		microjoules *= 1000
	case "uJ":
	case "nJ":
		microjoules /= 1000
	default:
		return nil
	}
	return energyWatts(0, microjoules, 0, elapsed)
}

func (r *ioReportSampler) update(channels []ioEnergy, now time.Time) map[string][]PowerReading {
	// Keep discovered channels explicit if they disappear, including after a
	// failed query. A partial die set must never become a smaller domain total.
	if r.names == nil {
		r.names = map[string]bool{}
	}
	seen := map[string]bool{}
	for _, c := range channels {
		if ioEnergyKind(c.name) != "" {
			r.names[c.name], seen[c.name] = true, true
		}
	}
	for name := range r.names {
		if !seen[name] {
			channels = append(channels, ioEnergy{name: name, value: -1})
		}
	}
	sort.Slice(channels, func(i, j int) bool { return channels[i].name < channels[j].name })
	readings := map[string][]PowerReading{}
	next := map[string]ioEnergyPoint{}
	aggregates := map[string]bool{}
	for _, c := range channels {
		if !strings.HasPrefix(c.name, "DIE_") {
			aggregates[ioEnergyKind(c.name)] = true
		}
	}
	for _, c := range channels {
		kind := ioEnergyKind(c.name)
		if kind == "" || strings.HasPrefix(c.name, "DIE_") && aggregates[kind] {
			continue
		}
		var v *float64
		next[c.name] = ioEnergyPoint{c, now}
		if old, ok := r.previous[c.name]; ok {
			elapsed := now.Sub(old.at)
			v = ioEnergyWatts(old.ioEnergy, c, elapsed)
			// Back-to-back priming calls must not discard the usable baseline.
			if elapsed >= 0 && elapsed < 100*time.Millisecond && old.unit == c.unit && c.value >= old.value {
				next[c.name] = old
			}
		}
		readings[kind] = append(readings[kind], power("ioreport:"+c.name, c.name+" (IOReport estimate)", "energy-average", v))
	}
	r.previous = next // Missing channels need a fresh baseline when they return.
	return readings
}

func attachIOReportPower(devices []Device, readings map[string][]PowerReading) []Device {
	for _, kind := range []string{"cpu", "gpu"} {
		channels := readings[kind]
		if len(channels) == 0 {
			continue
		}
		var total float64
		valid := true
		for _, p := range channels {
			if p.Watts == nil {
				valid = false
			} else {
				total += *p.Watts
			}
		}
		var primary *float64
		// update chooses either the domain aggregate or separate, nonoverlapping dies.
		if valid {
			primary = watts(Number(total))
		}
		index := -1
		for i, d := range devices {
			if d.Kind == kind && strings.HasPrefix(d.ID, "smc:") {
				index = i
				break
			}
		}
		if index < 0 {
			index = len(devices)
			devices = append(devices, Device{ID: "ioreport:" + kind, Name: strings.ToUpper(kind) + " power", Kind: kind, Source: "IOReport Energy Model"})
		} else {
			devices[index].Source += "; IOReport Energy Model"
		}
		devices[index].Power = primary
		devices[index].PowerReadings = append(devices[index].PowerReadings, channels...)
	}
	return devices
}
