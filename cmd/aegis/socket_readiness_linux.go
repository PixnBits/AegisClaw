//go:build linux

package main

import (
	"net"
	"time"
)

// isControlSocketReady returns true if we can successfully dial the control socket.
// On Linux with abstract sockets there is no filesystem entry to stat, so we
// use a dial attempt with a short timeout as the readiness probe.
func isControlSocketReady(addr string) bool {
	conn, err := net.DialTimeout("unix", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
