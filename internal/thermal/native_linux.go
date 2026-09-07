package thermal

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

func TerminalFile(f *os.File) bool {
	if f == nil {
		return false
	}
	var t syscall.Termios
	_, _, e := syscall.Syscall6(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t)), 0, 0, 0)
	return e == 0
}
func nativeHardware(ctx context.Context, h *HardwareInfo) {
	raw, _ := os.ReadFile("/proc/cpuinfo")
	cores := map[string]bool{}
	for _, block := range strings.Split(string(raw), "\n\n") {
		values := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok {
				values[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
		if name := values["model name"]; name != "" {
			h.CPU = name
		}
		if name := values["Hardware"]; h.CPU == "unknown" && name != "" {
			h.CPU = name
		}
		if socket, ok := values["physical id"]; ok {
			if core, ok := values["core id"]; ok {
				cores[socket+":"+core] = true
			}
		}
	}
	h.PhysicalCores = len(cores)
	raw, _ = os.ReadFile("/proc/meminfo")
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "MemTotal:" {
			if v, e := strconv.ParseUint(f[1], 10, 64); e == nil {
				h.RAMGiB = Number(float64(v) / (1 << 20))
			}
		}
	}
}
func (s *cpuSampler) read(ctx context.Context) *float64 {
	raw, e := os.ReadFile("/proc/stat")
	if e != nil {
		return nil
	}
	total, idle, ok := parseCPUTicks(string(raw))
	if !ok {
		return nil
	}
	return s.update(total, idle)
}
func clockTicks() float64 {
	raw, _ := os.ReadFile("/proc/self/auxv")
	for i := 0; i+16 <= len(raw); i += 16 {
		if binary.LittleEndian.Uint64(raw[i:]) == 17 {
			n := binary.LittleEndian.Uint64(raw[i+8:])
			if n > 0 {
				return float64(n)
			}
		}
	}
	return 100 // Supported Linux targets use the Linux USER_HZ ABI.
}
func nativeProcesses(ctx context.Context) map[int32]processSample {
	result := map[int32]processSample{}
	entries, _ := os.ReadDir("/proc")
	hz := clockTicks()
	for _, e := range entries {
		if ctx.Err() != nil {
			break
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		if p, ok := parseProcessStat(string(raw), hz); ok {
			result[p.pid] = p
		}
	}
	return result
}
