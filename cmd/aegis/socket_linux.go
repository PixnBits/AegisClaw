//go:build linux

package main

// getControlSocketAddr returns the address for the daemon control socket.
//
// On Linux we prefer an abstract Unix socket (name starts with \0).
// Abstract sockets have no filesystem entry, so there are no permission or
// ownership problems when the daemon runs as root and normal users need to
// connect for `status`, `vm list`, `stop`, etc.
//
// This resolves the long-standing tension between "daemon must run as root
// for Firecracker" and "CLI should be usable by the normal user" without
// using /tmp or relaxing permissions on a filesystem socket.
//
// See docs/deviations.md for context and the spec conflict this addresses.
func getControlSocketAddr() string {
	// Abstract socket name. The leading \0 is the marker for abstract sockets.
	// Any process on the machine can connect if it knows the name.
	return "\x00aegis/daemon"
}
