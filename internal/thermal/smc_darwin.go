package thermal

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var smcAPI = sync.OnceValues(func() (map[string]uintptr, error) {
	// Keep these framework handles for the process lifetime.
	_, procs, err := macOpen("/System/Library/Frameworks/IOKit.framework/IOKit", []string{
		"IOServiceMatching", "IOServiceGetMatchingService", "IOServiceOpen", "IOServiceClose", "IOObjectRelease", "IOConnectCallStructMethod", "mach_task_self",
	})
	return procs, err
})

func (sampler *smcSampler) read(ctx context.Context) ([]Device, error) {
	procs, err := smcAPI()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, _ := syscall.BytePtrFromString("AppleSMC")
	matching := macCall(procs["IOServiceMatching"], uintptr(unsafe.Pointer(name)))
	if matching == 0 {
		return nil, fmt.Errorf("AppleSMC matching unavailable")
	}
	service := macCall(procs["IOServiceGetMatchingService"], 0, matching)
	if service == 0 {
		return nil, fmt.Errorf("AppleSMC service unavailable")
	}
	defer macCall(procs["IOObjectRelease"], service)
	var conn uint32
	status := macCall(procs["IOServiceOpen"], service, macCall(procs["mach_task_self"]), 0, uintptr(unsafe.Pointer(&conn)))
	if status != 0 || conn == 0 {
		return nil, fmt.Errorf("AppleSMC access unavailable (0x%x)", status)
	}
	defer macCall(procs["IOServiceClose"], uintptr(conn))
	call := func(in smcData) (smcData, bool) {
		var out smcData
		size := unsafe.Sizeof(out)
		if ctx.Err() != nil {
			return out, false
		}
		code := macCall(procs["IOConnectCallStructMethod"], uintptr(conn), 2, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in), uintptr(unsafe.Pointer(&out)), uintptr(unsafe.Pointer(&size)))
		return out, code == 0 && size == unsafe.Sizeof(out) && out.Result == 0
	}
	read := func(key string) *float64 {
		in := smcData{Key: binary.BigEndian.Uint32([]byte(key)), Command: 9}
		info, ok := call(in)
		if !ok || info.Size > 32 {
			return nil
		}
		in.Size, in.Type, in.Attributes, in.Command = info.Size, info.Type, info.Attributes, 5
		out, ok := call(in)
		if !ok {
			return nil
		}
		return smcTemperature(info.Type, info.Size, out.Bytes)
	}

	chip, _ := syscall.Sysctl("machdep.cpu.brand_string")
	silicon := strings.HasPrefix(chip, "Apple M")
	if !sampler.ready {
		var cpuKey string
		var gpuKeys []string
		cpuKeys := []string{"TC0D", "TCAD", "TC0E", "TC0F"}
		if silicon {
			cpuKeys = []string{"TCMz", "TCMb"}
		}
		for _, key := range cpuKeys {
			if read(key) != nil {
				cpuKey = key
				break
			}
		}
		if silicon {
			// Discover GPU die sensors instead of guessing per-core keys by chip name.
			// Firmware CPU aggregates above avoid inactive/repurposed per-core keys.
			out, ok := call(smcData{Key: 0x234b4559, Size: 4, Command: 5}) // #KEY
			if !ok {
				return nil, fmt.Errorf("AppleSMC key discovery failed")
			}
			n := binary.BigEndian.Uint32(out.Bytes[:4])
			if n == 0 || n > 65536 {
				return nil, fmt.Errorf("invalid AppleSMC key count: %d", n)
			}
			for i := uint32(0); i < n; i++ {
				out, ok := call(smcData{Command: 8, Index: i})
				if !ok {
					return nil, fmt.Errorf("AppleSMC key discovery interrupted or unavailable")
				}
				var key [4]byte
				binary.BigEndian.PutUint32(key[:], out.Key)
				if key[0] == 'T' && key[1] == 'g' && read(string(key[:])) != nil {
					gpuKeys = append(gpuKeys, string(key[:]))
				}
			}
		}
		sampler.cpuKey, sampler.gpuKeys, sampler.ready = cpuKey, gpuKeys, true
	}
	devices := []Device{}
	if sampler.cpuKey != "" {
		devices = append(devices, Device{ID: "smc:" + sampler.cpuKey, Name: "CPU die (" + sampler.cpuKey + ")", Kind: "cpu", Source: "AppleSMC", Temp: read(sampler.cpuKey)})
	}
	if len(sampler.gpuKeys) > 0 {
		devices = append(devices, Device{ID: "smc:gpu", Name: chip, Kind: "gpu", Source: "AppleSMC (hottest GPU sensor)", Temp: smcHottest(sampler.gpuKeys, read)})
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no readable CPU/GPU die temperatures from AppleSMC")
	}
	return devices, nil
}
