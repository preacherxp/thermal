package thermal

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type openCLLibrary struct {
	dll   *syscall.DLL
	procs map[string]*syscall.Proc
}

func loadOpenCL() (openCLLibrary, error) {
	root := os.Getenv("SystemRoot")
	if !filepath.IsAbs(root) {
		return openCLLibrary{}, fmt.Errorf("Windows system directory unavailable")
	}
	dll, err := syscall.LoadDLL(filepath.Join(root, "System32", "OpenCL.dll"))
	if err != nil {
		return openCLLibrary{}, fmt.Errorf("OpenCL GPU driver unavailable: %w", err)
	}
	lib := openCLLibrary{dll: dll, procs: map[string]*syscall.Proc{}}
	for _, name := range openCLSymbols {
		p, err := dll.FindProc(name)
		if err != nil {
			lib.close()
			return openCLLibrary{}, err
		}
		lib.procs[name] = p
	}
	return lib, nil
}

//go:uintptrescapes
func (l openCLLibrary) call(name string, args ...uintptr) uintptr {
	r, _, _ := l.procs[name].Call(args...)
	return r
}
func (l openCLLibrary) close() {
	if l.dll != nil {
		l.dll.Release()
	}
}
