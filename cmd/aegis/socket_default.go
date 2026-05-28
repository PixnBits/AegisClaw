//go:build !linux

package main

import "path/filepath"

// getControlSocketAddr returns the address for the daemon control socket.
//
// On non-Linux platforms we fall back to a conventional filesystem socket
// inside the user's home directory (matching the documented spec location).
//
// See docs/deviations.md for background on why Linux uses a different
// mechanism.
func getControlSocketAddr() string {
	home, _ := os.UserHomeDir() // safe fallback if this fails
	return filepath.Join(home, ".aegis/daemon.sock")
}
