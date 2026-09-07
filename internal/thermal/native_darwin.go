package thermal

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func TerminalFile(f *os.File) bool {
	if f == nil {
		return false
	}
	var t syscall.Termios
	_, _, e := syscall.Syscall6(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGETA, uintptr(unsafe.Pointer(&t)), 0, 0, 0)
	return e == 0
}
func nativeHardware(ctx context.Context, h *HardwareInfo) {
	if n, e := syscall.Sysctl("machdep.cpu.brand_string"); e == nil && n != "" {
		h.CPU = n
	}
	if n, e := syscall.SysctlUint32("hw.physicalcpu"); e == nil {
		h.PhysicalCores = int(n)
	}
	if raw, e := command(ctx, "/usr/sbin/sysctl", "-n", "hw.memsize"); e == nil {
		if n, e := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64); e == nil {
			h.RAMGiB = Number(float64(n) / (1 << 30))
		}
	}
}
func (s *cpuSampler) read(ctx context.Context) *float64 {
	// top is part of macOS; the second sample is the current one-second interval.
	raw, e := command(ctx, "/usr/bin/top", "-l", "2", "-s", "1", "-n", "0")
	if e != nil {
		return nil
	}
	return parseMacCPU(string(raw))
}

func nativeProcesses(ctx context.Context) map[int32]processSample {
	result := map[int32]processSample{}
	raw, e := command(ctx, "/bin/ps", "-A", "-o", "pid=,lstart=,time=,comm=")
	if e != nil {
		return result
	}
	at := time.Now()
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 8 {
			continue
		}
		pid, e := strconv.ParseInt(f[0], 10, 32)
		if e != nil {
			continue
		}
		seconds, ok := parseProcessTime(f[6])
		if !ok {
			continue
		}
		p := processSample{int32(pid), strings.Join(f[7:], " "), seconds, strings.Join(f[1:6], " "), at}
		result[p.pid] = p
	}
	return result
}
