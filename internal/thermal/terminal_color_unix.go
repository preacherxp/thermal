//go:build !windows

package thermal

import "os"

func terminalColor(f *os.File) bool { return TerminalFile(f) }
