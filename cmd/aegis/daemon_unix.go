// +build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setSetsid sets the Setsid flag on Unix-like platforms.
func setSetsid(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
