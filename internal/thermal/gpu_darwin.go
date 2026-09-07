package thermal

type openCLLibrary struct {
	handle uintptr
	procs  map[string]uintptr
}

func loadOpenCL() (openCLLibrary, error) {
	h, procs, err := macOpen("/System/Library/Frameworks/OpenCL.framework/OpenCL", openCLSymbols)
	return openCLLibrary{h, procs}, err
}

//go:uintptrescapes
func (l openCLLibrary) call(name string, args ...uintptr) uintptr {
	return macCall(l.procs[name], args...)
}
func (l openCLLibrary) close() {
	if l.handle != 0 {
		macCall(macDlclose, l.handle)
	}
}
