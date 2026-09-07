package tui

import (
	"io"
	"os"
	"thermal-cli/internal/thermal"
)

func Available(input *os.File, output, errors io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || os.Getenv("TERM") == "dumb" {
		return false
	}
	out, ok := output.(*os.File)
	errOut, okErr := errors.(*os.File)
	return ok && okErr && thermal.TerminalFile(input) && thermal.TerminalFile(out) && thermal.TerminalFile(errOut)
}
