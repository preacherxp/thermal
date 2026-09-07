package thermal

//go:uintptrescapes
func macCallLong(fn uintptr, a [9]uintptr) uintptr {
	r, _, _ := macSyscall9(fn, a[0], a[1], a[2], a[3], a[4], a[5], a[6], a[7], a[8])
	return r
}
