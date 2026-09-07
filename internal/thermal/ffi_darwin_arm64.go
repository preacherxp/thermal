package thermal

func thermal_call8_trampoline()

var macCall8 uintptr

//go:uintptrescapes
func macCallLong(fn uintptr, a [9]uintptr) uintptr {
	// OpenCL's ninth argument is always a null event output. The runtime's
	// syscall9 puts argument nine in R8; Apple's ABI requires it on the stack.
	if a[8] != 0 {
		panic("native ninth argument must be null")
	}
	r, _, _ := macSyscall9(macCall8, fn, a[0], a[1], a[2], a[3], a[4], a[5], a[6], a[7])
	return r
}
