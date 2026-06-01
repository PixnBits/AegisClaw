//go:build linux

package main

import (
	"net"

	"github.com/mdlayher/vsock"
)

// tryVsockListen attempts to create a vsock listener on the given port.
// Used exclusively by the web-portal binary when running inside a Firecracker
// microVM so the Host Daemon reverse proxy can reach the HTTP handler over
// the vsock device (web-portal-vm.md §Networking + Inbound traffic rules).
// The port (18080) is chosen to match the internal TCP listen addr for simplicity.
// Returns (nil, nil) or error on any failure; caller treats non-nil listener as success.
func tryVsockListen(port uint32) (net.Listener, error) {
	return vsock.Listen(port, nil)
}
