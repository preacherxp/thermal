package thermal

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

type GPUInfo struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	DriverMaxWatts *float64 `json:"driver_max_w,omitempty"`
	EnforcedWatts  *float64 `json:"enforced_w,omitempty"`
}
type HardwareInfo struct {
	Manufacturer  string    `json:"manufacturer"`
	Model         string    `json:"model"`
	Family        string    `json:"family"`
	CPU           string    `json:"cpu"`
	PhysicalCores int       `json:"physical_cores"`
	LogicalCPUs   int       `json:"logical_cpus"`
	RAMGiB        *float64  `json:"ram_gib,omitempty"`
	GPUs          []GPUInfo `json:"gpus"`
	Warnings      []string  `json:"warnings,omitempty"`
}

func DetectHardware(ctx context.Context) HardwareInfo {
	h := HardwareInfo{Manufacturer: "unknown", Model: "unknown", CPU: "unknown", LogicalCPUs: runtime.NumCPU(), GPUs: []GPUInfo{}}
	nativeHardware(ctx, &h)
	nvidia, _ := NVIDIA(ctx)
	for _, d := range nvidia {
		gpu := GPUInfo{ID: d.ID, Name: d.Name}
		for _, p := range d.PowerReadings {
			if p.ID == "power.max_limit" {
				gpu.DriverMaxWatts = p.Watts
			}
			if p.ID == "enforced.power.limit" {
				gpu.EnforcedWatts = p.Watts
			}
		}
		h.GPUs = append(h.GPUs, gpu)
	}
	switch runtime.GOOS {
	case "windows":
		manufacturer, model, family, e := windowsModel()
		if e == nil {
			h.Manufacturer = manufacturer
			h.Model = model
			h.Family = family
		} else {
			h.Warnings = append(h.Warnings, "System model unavailable from Windows firmware registry")
		}
		if len(h.GPUs) == 0 {
			h.GPUs = append(h.GPUs, windowsGPUs()...)
		}
	case "linux":
		h.Manufacturer = readLabel("/sys/class/dmi/id/sys_vendor")
		h.Model = readLabel("/sys/class/dmi/id/product_name")
		h.Family = readLabel("/sys/class/dmi/id/product_version")
		paths, _ := filepath.Glob("/sys/class/drm/card*/device")
		for _, p := range paths {
			vendor := readLabel(filepath.Join(p, "vendor"))
			device := readLabel(filepath.Join(p, "device"))
			if vendor == "" || device == "" || vendor == "0x10de" && len(nvidia) > 0 {
				continue
			}
			real, _ := filepath.EvalSymlinks(p)
			id := filepath.Base(real)
			name := fmt.Sprintf("PCI GPU %s:%s", vendor, device)
			if raw, e := optionalPCIName(ctx, id); e == nil {
				line := strings.TrimSpace(string(raw))
				if i := strings.Index(line, ": "); i >= 0 {
					name = line[i+2:]
				}
			}
			duplicate := false
			for _, g := range h.GPUs {
				if g.ID == "pci:"+id {
					duplicate = true
				}
			}
			if !duplicate {
				h.GPUs = append(h.GPUs, GPUInfo{ID: "pci:" + id, Name: name})
			}
		}
	case "darwin":
		h.Manufacturer = "Apple"
		if raw, e := command(ctx, "/usr/sbin/sysctl", "-n", "hw.model"); e == nil {
			h.Model = strings.TrimSpace(string(raw))
		}
		if raw, e := command(ctx, "/usr/sbin/system_profiler", "SPDisplaysDataType", "-json"); e == nil {
			var x struct {
				Displays []struct {
					Name string `json:"sppci_model"`
				} `json:"SPDisplaysDataType"`
			}
			if json.Unmarshal(raw, &x) == nil {
				for i, g := range x.Displays {
					if g.Name != "" {
						h.GPUs = append(h.GPUs, GPUInfo{ID: fmt.Sprintf("apple-display:%d", i), Name: g.Name})
					}
				}
			}
		}
	}
	if h.Model == "" {
		h.Model = "unknown"
	}
	if h.Manufacturer == "" {
		h.Manufacturer = "unknown"
	}
	if len(h.GPUs) == 0 {
		h.Warnings = append(h.Warnings, "GPU model unavailable; no GPU-specific expectations will be guessed")
	}
	return h
}

func optionalPCIName(ctx context.Context, id string) ([]byte, error) {
	if !externalProviders() {
		return nil, fmt.Errorf("external providers disabled")
	}
	return command(ctx, "lspci", "-s", id)
}
