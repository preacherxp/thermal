//go:build !windows

package thermal

import "os/exec"

func configureCommand(cmd *exec.Cmd) {}
