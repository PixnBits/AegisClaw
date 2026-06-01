//go:build !linux

package main

import "net"

// tryVsockListen is the no-op stub on non-Linux platforms (darwin, windows, etc.).
// On these platforms the web-portal runs either as a host binary (E2E/fixture) or
// inside a Docker Sandbox (which uses ExposedPorts + TCP for the host proxy).
// The real vsock implementation lives in the linux-tagged file.
// Always returns (nil, nil) so callers silently skip the vsock path.
func tryVsockListen(port uint32) (net.Listener, error) {
	return nil, nil
}
