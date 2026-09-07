package thermal

// Read-only NVIDIA NVML driver calls. ABI reference:
// https://github.com/NVIDIA/go-nvml/blob/main/pkg/nvml/nvml.h
// No SDK, cgo, copied wrapper library, or helper executable is required.
import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

var nvml = sync.OnceValues(func() (*syscall.DLL, error) {
	root := os.Getenv("SystemRoot")
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("Windows system directory unavailable")
	}
	paths := []string{filepath.Join(root, "System32", "nvml.dll")}
	extra, _ := filepath.Glob(filepath.Join(root, "System32", "DriverStore", "FileRepository", "*", "nvml.dll"))
	paths = append(paths, extra...)
	for _, path := range paths {
		dll, e := syscall.LoadDLL(path)
		if e != nil {
			continue
		}
		init, e := dll.FindProc("nvmlInit_v2")
		if e != nil {
			dll.Release()
			continue
		}
		if r, _, _ := init.Call(); r != 0 {
			dll.Release()
			continue
		}
		// The singleton holds this driver reference until process exit.
		return dll, nil
	}
	return nil, fmt.Errorf("NVIDIA NVML driver API unavailable")
})

//go:uintptrescapes
func nvCall(dll *syscall.DLL, name string, args ...uintptr) bool {
	p, e := dll.FindProc(name)
	if e != nil {
		return false
	}
	r, _, _ := p.Call(args...)
	return r == 0
}
func nvString(dll *syscall.DLL, name string, h uintptr) string {
	var buf [256]byte
	if !nvCall(dll, name, h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))) {
		return ""
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf[:])
}
func nvUint(dll *syscall.DLL, name string, h uintptr) *float64 {
	var n uint32
	if !nvCall(dll, name, h, uintptr(unsafe.Pointer(&n))) {
		return nil
	}
	return Number(float64(n))
}
func milliwatts(n *float64) *float64 {
	if n == nil {
		return nil
	}
	return watts(Number(*n / 1000))
}

type nvField struct {
	ID, Scope          uint32
	Timestamp, Latency int64
	Type, Status       uint32
	Value              uint64
}

func nvFieldWatts(f nvField) *float64 {
	if f.Status != 0 {
		return nil
	}
	var v float64
	switch f.Type {
	case 0:
		v = math.Float64frombits(f.Value)
	case 1, 2:
		v = float64(uint32(f.Value)) // Windows unsigned long is 32 bits.
	case 3:
		v = float64(f.Value)
	case 4:
		v = float64(int64(f.Value))
	case 5:
		v = float64(int32(f.Value))
	case 6:
		v = float64(uint16(f.Value))
	default:
		return nil
	}
	return watts(Number(v / 1000))
}
func nativeNVIDIA() ([]Device, error) {
	dll, e := nvml()
	if e != nil {
		return nil, e
	}
	var count uint32
	if !nvCall(dll, "nvmlDeviceGetCount_v2", uintptr(unsafe.Pointer(&count))) {
		return nil, fmt.Errorf("NVIDIA device enumeration unavailable")
	}
	var result []Device
	for i := uint32(0); i < count; i++ {
		var h uintptr
		if !nvCall(dll, "nvmlDeviceGetHandleByIndex_v2", uintptr(i), uintptr(unsafe.Pointer(&h))) {
			continue
		}
		id := nvString(dll, "nvmlDeviceGetUUID", h)
		if id == "" {
			continue
		}
		d := Device{ID: "nvidia:" + id, Name: nvString(dll, "nvmlDeviceGetName", h), Kind: "gpu", Source: "NVIDIA NVML"}
		var temp, clock uint32
		if nvCall(dll, "nvmlDeviceGetTemperature", h, 0, uintptr(unsafe.Pointer(&temp))) {
			d.Temp = temperature(Number(float64(temp)))
		}
		if nvCall(dll, "nvmlDeviceGetClockInfo", h, 0, uintptr(unsafe.Pointer(&clock))) {
			d.Clock = Number(float64(clock))
		}
		var util struct{ GPU, Memory uint32 }
		if nvCall(dll, "nvmlDeviceGetUtilizationRates", h, uintptr(unsafe.Pointer(&util))) && util.GPU <= 100 {
			d.Util = Number(float64(util.GPU))
		}
		var reasons, supported uint64
		if nvCall(dll, "nvmlDeviceGetCurrentClocksThrottleReasons", h, uintptr(unsafe.Pointer(&reasons))) && nvCall(dll, "nvmlDeviceGetSupportedClocksThrottleReasons", h, uintptr(unsafe.Pointer(&supported))) {
			if reasons&0x60 != 0 {
				v := true
				d.Throttled = &v
			} else if supported&0x60 == 0x60 {
				v := false
				d.Throttled = &v
			}
		}
		d.Power = milliwatts(nvUint(dll, "nvmlDeviceGetPowerUsage", h))
		d.PowerReadings = []PowerReading{power("power.draw", "Board draw (driver)", "draw", d.Power)}
		fields := []nvField{{ID: 186, Status: 999}, {ID: 185, Status: 999}, {ID: 192, Status: 999}, {ID: 190, Status: 999}, {ID: 189, Status: 999}, {ID: 187, Status: 999}, {ID: 188, Status: 999}}
		fieldOK := nvCall(dll, "nvmlDeviceGetFieldValues", h, uintptr(len(fields)), uintptr(unsafe.Pointer(&fields[0])))
		var minimum, maximum uint32
		constraints := nvCall(dll, "nvmlDeviceGetPowerManagementLimitConstraints", h, uintptr(unsafe.Pointer(&minimum)), uintptr(unsafe.Pointer(&maximum)))
		for j, key := range nvidiaPowerFields {
			var v *float64
			if fieldOK {
				v = nvFieldWatts(fields[j])
			}
			if v == nil {
				switch j {
				case 3:
					v = milliwatts(nvUint(dll, "nvmlDeviceGetEnforcedPowerLimit", h))
				case 4:
					v = milliwatts(nvUint(dll, "nvmlDeviceGetPowerManagementDefaultLimit", h))
				case 5:
					if constraints {
						v = Number(float64(minimum) / 1000)
					}
				case 6:
					if constraints {
						v = Number(float64(maximum) / 1000)
					}
				}
			}
			mode := "limit"
			if j == 0 {
				mode = "instant"
			} else if j == 1 {
				mode = "average"
			}
			d.PowerReadings = append(d.PowerReadings, power(key, nvidiaPowerNames[j], mode, v))
		}
		result = append(result, d)
	}
	return result, nil
}
