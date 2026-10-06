//go:build !linux

package unixsock

import "net"

// PeerUID reports the peer euid on Linux. Other platforms have no
// SO_PEERCRED lookup here and return (-1, false).
func PeerUID(conn net.Conn) (int, bool) {
	if _, ok := conn.(*net.UnixConn); !ok {
		return -1, false
	}
	return -1, false
}
