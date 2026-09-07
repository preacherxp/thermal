package thermal

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Use the same runtime C-ABI bridge as Go's Darwin syscall package. All calls
// here have integer/pointer arguments and no callbacks into Go.
//
//go:linkname macSyscall6 syscall.syscall6X
//go:uintptrescapes
func macSyscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)

//go:linkname macSyscall9 syscall.syscall9
//go:uintptrescapes
func macSyscall9(fn, a1, a2, a3, a4, a5, a6, a7, a8, a9 uintptr) (r1, r2 uintptr, err syscall.Errno)

//go:cgo_import_dynamic thermal_dlopen dlopen "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic thermal_dlsym dlsym "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic thermal_dlclose dlclose "/usr/lib/libSystem.B.dylib"
func thermal_dlopen_trampoline()
func thermal_dlsym_trampoline()
func thermal_dlclose_trampoline()

var macDlopen, macDlsym, macDlclose uintptr

//go:uintptrescapes
func macCall(fn uintptr, args ...uintptr) uintptr {
	var a [9]uintptr
	if len(args) > len(a) {
		panic("too many native arguments")
	}
	copy(a[:], args)
	if len(args) <= 6 {
		r, _, _ := macSyscall6(fn, a[0], a[1], a[2], a[3], a[4], a[5])
		return r
	}
	return macCallLong(fn, a)
}

func macOpen(path string, names []string) (uintptr, map[string]uintptr, error) {
	p, err := syscall.BytePtrFromString(path)
	if err != nil {
		return 0, nil, err
	}
	handle := macCall(macDlopen, uintptr(unsafe.Pointer(p)), 2) // RTLD_NOW
	if handle == 0 {
		return 0, nil, fmt.Errorf("macOS framework unavailable: %s", path)
	}
	procs := make(map[string]uintptr, len(names))
	for _, name := range names {
		p, err := syscall.BytePtrFromString(name)
		if err != nil {
			macCall(macDlclose, handle)
			return 0, nil, err
		}
		fn := macCall(macDlsym, handle, uintptr(unsafe.Pointer(p)))
		if fn == 0 {
			macCall(macDlclose, handle)
			return 0, nil, fmt.Errorf("macOS symbol unavailable: %s", name)
		}
		procs[name] = fn
	}
	return handle, procs, nil
}
