package thermal

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

var kernelDLL = syscall.NewLazyDLL(filepath.Join(os.Getenv("SystemRoot"), "System32", "kernel32.dll"))
var systemTimes = kernelDLL.NewProc("GetSystemTimes")
var memoryStatus = kernelDLL.NewProc("GlobalMemoryStatusEx")
var processorInfo = kernelDLL.NewProc("GetLogicalProcessorInformationEx")

func TerminalFile(f *os.File) bool {
	if f == nil {
		return false
	}
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(f.Fd()), &mode) == nil
}
func registryString(path, name string) (string, error) {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return "", e
	}
	var key syscall.Handle
	if e = syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, p, 0, syscall.KEY_QUERY_VALUE, &key); e != nil {
		return "", e
	}
	defer syscall.RegCloseKey(key)
	n, e := syscall.UTF16PtrFromString(name)
	if e != nil {
		return "", e
	}
	var size, kind uint32
	if e = syscall.RegQueryValueEx(key, n, nil, &kind, nil, &size); e != nil {
		return "", e
	}
	if (kind != syscall.REG_SZ && kind != syscall.REG_EXPAND_SZ) || size < 2 || size > 65536 || size%2 != 0 {
		return "", fmt.Errorf("invalid registry string")
	}
	buf := make([]uint16, size/2)
	if e = syscall.RegQueryValueEx(key, n, nil, &kind, (*byte)(unsafe.Pointer(&buf[0])), &size); e != nil {
		return "", e
	}
	return syscall.UTF16ToString(buf), nil
}
func windowsModel() (string, string, string, error) {
	const key = "HARDWARE\\DESCRIPTION\\System\\BIOS"
	m, e := registryString(key, "SystemManufacturer")
	model, _ := registryString(key, "SystemProductName")
	family, _ := registryString(key, "SystemFamily")
	return m, model, family, e
}
func nativeHardware(ctx context.Context, h *HardwareInfo) {
	if name, e := registryString("HARDWARE\\DESCRIPTION\\System\\CentralProcessor\\0", "ProcessorNameString"); e == nil {
		h.CPU = name
	}
	var mem struct {
		Length, Load                                                                                         uint32
		TotalPhys, AvailPhys, TotalPageFile, AvailPageFile, TotalVirtual, AvailVirtual, AvailExtendedVirtual uint64
	}
	mem.Length = uint32(unsafe.Sizeof(mem))
	if r, _, _ := memoryStatus.Call(uintptr(unsafe.Pointer(&mem))); r != 0 {
		h.RAMGiB = Number(float64(mem.TotalPhys) / (1 << 30))
	}
	var size uint32
	processorInfo.Call(0, 0, uintptr(unsafe.Pointer(&size)))
	if size >= 8 && size < 1<<24 {
		buf := make([]byte, size)
		if r, _, _ := processorInfo.Call(0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r != 0 {
			for i := 0; i+8 <= int(size); {
				length := int(binary.LittleEndian.Uint32(buf[i+4:]))
				if length < 8 || i+length > int(size) {
					break
				}
				if binary.LittleEndian.Uint32(buf[i:]) == 0 {
					h.PhysicalCores++
				}
				i += length
			}
		}
	}
}
func fileTicks(t syscall.Filetime) uint64 { return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime) }
func (s *cpuSampler) read(ctx context.Context) *float64 {
	// GetSystemTimes is group-local on systems with more than 64 logical CPUs.
	if runtime.NumCPU() > 64 {
		return nil
	}
	var idle, kernel, user syscall.Filetime
	if r, _, _ := systemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user))); r == 0 {
		return nil
	}
	return s.update(fileTicks(kernel)+fileTicks(user), fileTicks(idle))
}
func nativeProcesses(ctx context.Context) map[int32]processSample {
	result := map[int32]processSample{}
	snapshot, e := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if e != nil {
		return result
	}
	defer syscall.CloseHandle(snapshot)
	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for e = syscall.Process32First(snapshot, &entry); e == nil; e = syscall.Process32Next(snapshot, &entry) {
		if ctx.Err() != nil {
			break
		}
		handle, e := syscall.OpenProcess(0x1000, false, entry.ProcessID)
		if e != nil {
			continue
		}
		var creation, exit, kernel, user syscall.Filetime
		e = syscall.GetProcessTimes(handle, &creation, &exit, &kernel, &user)
		syscall.CloseHandle(handle)
		if e == nil {
			pid := int32(entry.ProcessID)
			result[pid] = processSample{pid, syscall.UTF16ToString(entry.ExeFile[:]), float64(fileTicks(kernel)+fileTicks(user)) / 1e7, strconv.FormatUint(fileTicks(creation), 10), time.Now()}
		}
	}
	return result
}
func windowsGPUs() []GPUInfo {
	dll := syscall.NewLazyDLL(filepath.Join(os.Getenv("SystemRoot"), "System32", "user32.dll"))
	proc := dll.NewProc("EnumDisplayDevicesW")
	var result []GPUInfo
	for i := uintptr(0); i < 64; i++ {
		var d struct {
			Size        uint32
			Name        [32]uint16
			Description [128]uint16
			Flags       uint32
			ID          [128]uint16
			Key         [128]uint16
		}
		d.Size = uint32(unsafe.Sizeof(d))
		r, _, _ := proc.Call(0, i, uintptr(unsafe.Pointer(&d)), 0)
		if r == 0 {
			break
		}
		if d.Flags&8 != 0 {
			continue
		}
		result = append(result, GPUInfo{ID: "display:" + syscall.UTF16ToString(d.ID[:]), Name: syscall.UTF16ToString(d.Description[:])})
	}
	return result
}
