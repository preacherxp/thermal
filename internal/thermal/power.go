package thermal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Channels overlap (e.g. CPU package includes cores); never sum them.
type PowerReading struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Watts *float64 `json:"watts"`
	Mode  string   `json:"mode"`
}

func watts(v *float64) *float64 {
	if v == nil || *v < 0 {
		return nil
	}
	return Number(*v)
}
func power(id, name, mode string, v *float64) PowerReading {
	return PowerReading{id, name, watts(v), mode}
}

var nvidiaPowerFields = []string{"power.draw.instant", "power.draw.average", "power.limit", "enforced.power.limit", "power.default_limit", "power.min_limit", "power.max_limit"}
var nvidiaPowerNames = []string{"Board instantaneous", "Board average (1 second)", "Requested limit", "Enforced limit", "Default limit", "Minimum configurable limit", "Maximum configurable limit"}

func hardwareKind(parent string) string {
	parent = strings.ToLower(parent)
	if strings.Contains(parent, "cpu") {
		return "cpu"
	}
	if strings.Contains(parent, "gpu") {
		return "gpu"
	}
	return "other"
}
func ParseLHM(raw []byte) ([]Device, error) {
	var rows []struct {
		Identifier, Name, Parent, SensorType string
		Value                                *float64
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Identifier < rows[j].Identifier })
	ds := []Device{}
	anchors := map[string]int{}
	for _, r := range rows {
		if r.SensorType != "Temperature" {
			continue
		}
		ds = append(ds, Device{ID: "lhm:" + r.Identifier, Name: r.Name, Kind: hardwareKind(r.Parent), Source: "LibreHardwareMonitor", Temp: temperature(r.Value)})
		_, exists := anchors[r.Parent]
		if !exists || strings.Contains(strings.ToLower(r.Name), "package") {
			anchors[r.Parent] = len(ds) - 1
		}
	}
	for _, r := range rows {
		if r.SensorType != "Power" {
			continue
		}
		idx, ok := anchors[r.Parent]
		if !ok {
			idx = len(ds)
			anchors[r.Parent] = idx
			ds = append(ds, Device{ID: "lhm:" + r.Parent + "/power", Name: strings.ToUpper(hardwareKind(r.Parent)) + " " + r.Parent, Kind: hardwareKind(r.Parent), Source: "LibreHardwareMonitor"})
		}
		d := &ds[idx]
		d.PowerReadings = append(d.PowerReadings, power(r.Identifier, r.Name, "draw", r.Value))
		label := strings.ToLower(r.Name)
		if label == "cpu package" || label == "gpu package" || label == "gpu power" || label == "gpu board" {
			d.Power = watts(r.Value)
		}
	}
	return ds, nil
}
func ParseMacmon(raw []byte) ([]Device, error) {
	var row struct {
		Temp struct {
			CPU *float64 `json:"cpu_temp_avg"`
			GPU *float64 `json:"gpu_temp_avg"`
		} `json:"temp"`
		CPU      *float64 `json:"cpu_power"`
		GPU      *float64 `json:"gpu_power"`
		GPURAM   *float64 `json:"gpu_ram_power"`
		RAM      *float64 `json:"ram_power"`
		ANE      *float64 `json:"ane_power"`
		System   *float64 `json:"sys_power"`
		Combined *float64 `json:"all_power"`
		GPUClock *float64 `json:"gpu_freq_mhz"`
	}
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, err
	}
	return []Device{
		{ID: "macmon:cpu", Name: "Apple CPU average", Kind: "cpu", Source: "macmon", Temp: temperature(row.Temp.CPU), Power: watts(row.CPU), PowerReadings: []PowerReading{power("cpu", "CPU domain", "average", row.CPU)}},
		{ID: "macmon:gpu", Name: "Apple GPU average", Kind: "gpu", Source: "macmon", Temp: temperature(row.Temp.GPU), Power: watts(row.GPU), Clock: row.GPUClock, PowerReadings: []PowerReading{power("gpu", "GPU domain", "average", row.GPU), power("gpu-ram", "GPU SRAM", "average", row.GPURAM)}},
		{ID: "macmon:soc", Name: "Apple SoC / system", Kind: "other", Source: "macmon", PowerReadings: []PowerReading{power("ram", "RAM", "average", row.RAM), power("ane", "Neural engine", "average", row.ANE), power("combined", "Combined CPU/GPU/ANE (overlaps)", "average", row.Combined), power("system", "System (provider estimate)", "average", row.System)}},
	}, nil
}

type energyPoint struct {
	energy float64
	at     time.Time
}
type raplSampler struct{ previous map[string]energyPoint }

func readNum(path string) *float64 {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil
	}
	return parseNumber(string(b))
}
func readLabel(path string) string { b, _ := os.ReadFile(path); return strings.TrimSpace(string(b)) }
func energyWatts(previous, current, maximum float64, elapsed time.Duration) *float64 {
	if elapsed < 100*time.Millisecond || elapsed > time.Minute || previous < 0 || current < 0 {
		return nil
	}
	delta := current - previous
	if delta < 0 {
		if maximum <= previous || maximum <= current {
			return nil
		}
		delta = maximum - previous + current
	}
	return watts(Number(delta / 1e6 / elapsed.Seconds()))
}
func (r *raplSampler) read(root string, now time.Time) []Device {
	if r.previous == nil {
		r.previous = map[string]energyPoint{}
	}
	seen := map[string]bool{}
	ds := []Device{}
	var visit func(string, string, int)
	visit = func(dir, parent string, depth int) {
		if depth > 5 {
			return
		}
		canonical, e := filepath.EvalSymlinks(dir)
		if e != nil || seen[canonical] {
			return
		}
		seen[canonical] = true
		name := readLabel(filepath.Join(dir, "name"))
		label := name
		if parent != "" && name != "" {
			label = parent + " / " + name
		}
		if name != "" {
			id := "rapl:" + canonical
			d := Device{ID: id, Name: "CPU " + label, Kind: "cpu", Source: "linux-rapl"}
			current := readNum(filepath.Join(dir, "energy_uj"))
			if current != nil && *current >= 0 {
				if previous, ok := r.previous[id]; ok {
					maximum := 0.0
					if m := readNum(filepath.Join(dir, "max_energy_range_uj")); m != nil {
						maximum = *m
					}
					d.Power = energyWatts(previous.energy, *current, maximum, now.Sub(previous.at))
					if now.Sub(previous.at) >= 100*time.Millisecond {
						r.previous[id] = energyPoint{*current, now}
					}
				} else {
					r.previous[id] = energyPoint{*current, now}
				}
			} else {
				delete(r.previous, id)
			}
			d.PowerReadings = []PowerReading{power("domain", label+" (interval average)", "energy-average", d.Power)}
			paths, _ := filepath.Glob(filepath.Join(dir, "constraint_*_power_limit_uw"))
			for _, p := range paths {
				stem := strings.TrimSuffix(filepath.Base(p), "_power_limit_uw")
				n := readLabel(filepath.Join(dir, stem+"_name"))
				if n == "" {
					n = stem
				}
				v := readNum(p)
				if v != nil {
					*v /= 1e6
				}
				d.PowerReadings = append(d.PowerReadings, power(stem, n+" limit", "limit", v))
			}
			ds = append(ds, d)
		}
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if strings.Contains(entry.Name(), "rapl") {
				visit(filepath.Join(dir, entry.Name()), label, depth+1)
			}
		}
	}
	visit(root, "", 0)
	active := map[string]bool{}
	for _, d := range ds {
		active[d.ID] = true
	}
	for id := range r.previous {
		if !active[id] {
			delete(r.previous, id)
		}
	}
	return ds
}
func linuxPower(root string) []Device {
	paths, _ := filepath.Glob(filepath.Join(root, "hwmon*", "power*_*"))
	groups := map[string]*Device{}
	for _, p := range paths {
		base := filepath.Base(p)
		mode := ""
		if strings.HasSuffix(base, "_input") {
			mode = "instant"
		} else if strings.HasSuffix(base, "_average") {
			mode = "average"
		} else {
			continue
		}
		dir := filepath.Dir(p)
		real, _ := filepath.EvalSymlinks(dir)
		if real == "" {
			real = dir
		}
		if i := strings.LastIndex(real, "/hwmon/"); i >= 0 {
			real = real[:i]
		}
		driver := readLabel(filepath.Join(dir, "name"))
		kind := "other"
		if driver == "amdgpu" || driver == "nouveau" {
			kind = "gpu"
		} else if driver == "coretemp" || driver == "k10temp" || driver == "zenpower" {
			kind = "cpu"
		}
		d := groups[real]
		if d == nil {
			d = &Device{ID: "hwmon-power:" + real, Name: driver + " power (primary: power1)", Kind: kind, Source: "linux-hwmon"}
			groups[real] = d
		}
		stem := strings.TrimSuffix(strings.TrimSuffix(base, "_input"), "_average")
		label := readLabel(filepath.Join(dir, stem+"_label"))
		if label == "" {
			label = stem
		}
		v := readNum(p)
		if v != nil {
			*v /= 1e6
		}
		d.PowerReadings = append(d.PowerReadings, power(base, label+" ("+mode+")", mode, v))
		if stem == "power1" && (d.Power == nil || mode == "average") {
			d.Power = watts(v)
		}
	}
	ds := []Device{}
	for _, d := range groups {
		ds = append(ds, *d)
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i].ID < ds[j].ID })
	return ds
}
func PowerWarnings(ds []Device) []string {
	out := []string{}
	for _, kind := range []string{"cpu", "gpu"} {
		known := false
		for _, d := range ds {
			if d.Kind != kind {
				continue
			}
			if d.Power != nil {
				known = true
			}
			for _, p := range d.PowerReadings {
				if p.Mode != "limit" && p.Watts != nil {
					known = true
				}
			}
		}
		if !known {
			out = append(out, fmt.Sprintf("%s wattage unavailable: a readable power sensor/provider is required", strings.ToUpper(kind)))
		}
	}
	return out
}
func Snapshot(ctx context.Context, reader Reader) (Sample, []string) {
	reader.Read(ctx)
	select {
	case <-ctx.Done():
		return Sample{}, []string{"Sensor check interrupted"}
	case <-time.After(250 * time.Millisecond):
	}
	return reader.Read(ctx)
}
