package thermal

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Reader interface {
	Read(context.Context) (Sample, []string)
}
type Sensors struct {
	rapl raplSampler
	cpu  cpuSampler
}

func NewSensors() *Sensors { s := &Sensors{}; s.cpu.read(context.Background()); return s }

func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, name, args...)
	configureCommand(cmd)
	return cmd.Output()
}
func parseNumber(s string) *float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	return Number(v)
}
func temperature(p *float64) *float64 {
	if p != nil && (*p < -30 || *p > 150) {
		return nil
	}
	return p
}
func boolFlag(s string) *bool {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "active" || s == "yes" || s == "true" {
		b := true
		return &b
	}
	if s == "not active" || s == "no" || s == "false" {
		b := false
		return &b
	}
	return nil
}
func nvidiaExecutable() string {
	if p := os.Getenv("THERMAL_NVIDIA_SMI"); p != "" {
		return p
	}
	if p, e := exec.LookPath("nvidia-smi"); e == nil {
		return p
	}
	if runtime.GOOS == "windows" {
		for _, p := range []string{filepath.Join(os.Getenv("SystemRoot"), "System32", "nvidia-smi.exe"), filepath.Join(os.Getenv("ProgramFiles"), "NVIDIA Corporation", "NVSMI", "nvidia-smi.exe")} {
			if _, e := os.Stat(p); e == nil {
				return p
			}
		}
		matches, _ := filepath.Glob(filepath.Join(os.Getenv("SystemRoot"), "System32", "DriverStore", "FileRepository", "*", "nvidia-smi.exe"))
		sort.Slice(matches, func(i, j int) bool {
			a, e1 := os.Stat(matches[i])
			b, e2 := os.Stat(matches[j])
			return e1 == nil && (e2 != nil || a.ModTime().After(b.ModTime()))
		})
		if len(matches) > 0 {
			return matches[0]
		}
	}
	return "nvidia-smi"
}

var nvidiaPath = sync.OnceValue(nvidiaExecutable)

func NVIDIA(ctx context.Context) ([]Device, error) {
	if ds, e := nativeNVIDIA(); e == nil && len(ds) > 0 {
		return ds, nil
	}
	if !externalProviders() {
		return nil, fmt.Errorf("NVIDIA telemetry unavailable from native driver interfaces")
	}
	fields := "uuid,name,temperature.gpu,utilization.gpu,power.draw,clocks.current.graphics"
	flags := ",clocks_throttle_reasons.sw_thermal_slowdown,clocks_throttle_reasons.hw_thermal_slowdown"
	queries := []string{fields + flags + "," + strings.Join(nvidiaPowerFields, ","), fields + flags + "," + strings.Join(nvidiaPowerFields[2:], ","), fields + flags, fields}
	var last error
	for _, query := range queries {
		out, err := command(ctx, nvidiaPath(), "--query-gpu="+query, "--format=csv,noheader,nounits")
		if err == nil {
			return ParseNVIDIA(string(out))
		}
		last = err
		if ctx.Err() != nil {
			break
		}
		var missing *exec.Error
		if errors.As(err, &missing) {
			break
		}
	}
	return nil, last
}
func ParseNVIDIA(text string) ([]Device, error) {
	rows, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil {
		return nil, err
	}
	ds := []Device{}
	for _, row := range rows {
		if len(row) < 6 {
			return nil, fmt.Errorf("unexpected nvidia-smi output")
		}
		d := Device{ID: "nvidia:" + strings.TrimSpace(row[0]), Name: strings.TrimSpace(row[1]), Kind: "gpu", Source: "nvidia-smi", Temp: temperature(parseNumber(row[2])), Util: parseNumber(row[3]), Power: watts(parseNumber(row[4])), Clock: parseNumber(row[5])}
		if len(row) >= 8 {
			a, b := boolFlag(row[6]), boolFlag(row[7])
			if (a != nil && *a) || (b != nil && *b) {
				v := true
				d.Throttled = &v
			} else if a != nil && b != nil {
				v := false
				d.Throttled = &v
			}
		}
		d.PowerReadings = []PowerReading{power("power.draw", "Board draw (driver)", "draw", d.Power)}
		for i, key := range nvidiaPowerFields {
			var v *float64
			if len(row) >= 15 {
				v = parseNumber(row[8+i])
			} else if len(row) == 13 && i >= 2 {
				v = parseNumber(row[6+i])
			}
			mode := "limit"
			if i == 0 {
				mode = "instant"
			} else if i == 1 {
				mode = "average"
			}
			d.PowerReadings = append(d.PowerReadings, power(key, nvidiaPowerNames[i], mode, v))
		}
		ds = append(ds, d)
	}
	return ds, nil
}
func linuxSensors() []Device {
	paths, _ := filepath.Glob("/sys/class/hwmon/hwmon*/temp*_input")
	var ds []Device
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		v := parseNumber(string(raw))
		if v == nil {
			continue
		}
		*v /= 1000
		if temperature(v) == nil {
			continue
		}
		root := filepath.Dir(p)
		name, _ := os.ReadFile(filepath.Join(root, "name"))
		driver := strings.TrimSpace(string(name))
		stem := strings.TrimSuffix(filepath.Base(p), "_input")
		label, _ := os.ReadFile(filepath.Join(root, stem+"_label"))
		title := strings.TrimSpace(string(label))
		if title == "" {
			title = stem
		}
		kind := "other"
		switch driver {
		case "coretemp", "k10temp", "zenpower", "cpu_thermal":
			kind = "cpu"
		case "amdgpu", "nouveau":
			kind = "gpu"
		}
		crit, _ := os.ReadFile(filepath.Join(root, stem+"_crit"))
		cp := parseNumber(string(crit))
		if cp != nil {
			*cp /= 1000
			if *cp <= 0 {
				cp = nil
			}
		}
		// Resolve the physical device, stripping the boot-dependent hwmon number.
		real, _ := filepath.EvalSymlinks(root)
		if real == "" {
			real = root
		}
		if i := strings.LastIndex(real, "/hwmon/"); i >= 0 {
			real = real[:i]
		}
		ds = append(ds, Device{ID: "hwmon:" + real + ":" + stem, Name: driver + " " + title, Kind: kind, Source: "linux-hwmon", Temp: v, Critical: cp})
	}
	return ds
}
func windowsSensors(ctx context.Context) ([]Device, error) {
	script := "$ErrorActionPreference='Stop'; @(Get-CimInstance -Namespace root/LibreHardwareMonitor -ClassName Sensor -Filter \"SensorType = 'Temperature' OR SensorType = 'Power'\" | Select-Object Identifier,Name,Parent,SensorType,Value) | ConvertTo-Json -Compress"
	raw, err := command(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return nil, err
	}
	return ParseLHM(raw)
}
func macSensors(ctx context.Context) ([]Device, error) {
	raw, err := command(ctx, "macmon", "pipe", "--samples", "1", "--interval", "100")
	if err != nil {
		return nil, err
	}
	return ParseMacmon(raw)
}
func (reader *Sensors) Read(ctx context.Context) (Sample, []string) {
	s := Sample{Devices: []Device{}}
	warnings := []string{}
	s.CPU = reader.cpu.read(ctx)
	if s.CPU == nil {
		warnings = append(warnings, "CPU utilization unavailable until valid samples are collected")
	}
	var ds []Device
	var err error
	switch runtime.GOOS {
	case "linux":
		ds = linuxSensors()
		ds = append(ds, linuxPower("/sys/class/hwmon")...)
		ds = append(ds, reader.rapl.read("/sys/class/powercap", time.Now())...)
	case "windows":
		if externalProviders() {
			ds, err = windowsSensors(ctx)
		} else {
			err = fmt.Errorf("native CPU telemetry unavailable")
		}
		if err != nil {
			warnings = append(warnings, "CPU temperature/power unavailable through native Windows APIs; optional providers can be enabled with THERMAL_EXTERNAL_PROVIDERS=1")
		}
	case "darwin":
		if externalProviders() {
			ds, err = macSensors(ctx)
		} else {
			err = fmt.Errorf("native thermal telemetry unavailable")
		}
		if err != nil {
			warnings = append(warnings, "Native macOS CPU/GPU temperature and power are unavailable; optional Apple Silicon macmon integration requires THERMAL_EXTERNAL_PROVIDERS=1")
		}
	}
	s.Devices = append(s.Devices, ds...)
	gpu, e := NVIDIA(ctx)
	if e == nil {
		s.Devices = append(s.Devices, gpu...)
	}
	warnings = append(warnings, PowerWarnings(s.Devices)...)
	if len(s.Devices) == 0 {
		warnings = append(warnings, "No temperature sensors found; temperatures and thermal health are unknown")
	}
	sort.Slice(s.Devices, func(i, j int) bool { return s.Devices[i].ID < s.Devices[j].ID })
	return s, warnings
}
