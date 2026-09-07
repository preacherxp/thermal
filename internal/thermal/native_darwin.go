package thermal

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
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

var macCPUAPI = sync.OnceValues(func() (map[string]uintptr, error) {
	_, procs, err := macOpen("/usr/lib/libSystem.B.dylib", []string{"mach_host_self", "host_statistics", "mach_task_self", "mach_port_deallocate"})
	return procs, err
})

func (s *cpuSampler) read(ctx context.Context) *float64 {
	if ctx.Err() != nil {
		return nil
	}
	procs, err := macCPUAPI()
	if err != nil {
		return nil
	}
	host := macCall(procs["mach_host_self"])
	if host == 0 {
		return nil
	}
	defer macCall(procs["mach_port_deallocate"], macCall(procs["mach_task_self"]), host)
	var ticks [4]uint32 // user, system, idle, nice
	count := uint32(len(ticks))
	if macCall(procs["host_statistics"], host, 3, uintptr(unsafe.Pointer(&ticks[0])), uintptr(unsafe.Pointer(&count))) != 0 || count != 4 {
		return nil
	}
	return s.update(uint64(ticks[0])+uint64(ticks[1])+uint64(ticks[2])+uint64(ticks[3]), uint64(ticks[2]))
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
