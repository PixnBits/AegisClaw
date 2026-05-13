//go:build windows
// +build windows

package main

import (
	"os/exec"
)

// setSetsid is a no-op on Windows (process groups handled differently).
func setSetsid(cmd *exec.Cmd) {
	// No-op on Windows
}
