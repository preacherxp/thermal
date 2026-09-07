package thermal

import (
	"os"
	"syscall"
)

// A Windows console handle alone does not imply ANSI support. Older hosts show
// escape sequences literally unless virtual terminal processing is enabled.
func terminalColor(f *os.File) bool {
	if f == nil {
		return false
	}
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(f.Fd()), &mode) == nil && mode&0x0004 != 0
}
