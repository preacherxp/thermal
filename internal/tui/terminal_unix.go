//go:build !windows

package tui

import "os"

func prepareTerminal(f *os.File) (func(), error) { return func() {}, nil }
