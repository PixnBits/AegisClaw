//go:build linux

package unixsock

import (
	"net"

	"golang.org/x/sys/unix"
)

// PeerUID returns the peer euid of a *net.UnixConn via SO_PEERCRED.
// Non-UNIX conns, and any lookup error, return (-1, false).
// The socket is left non-blocking.
func PeerUID(conn net.Conn) (int, bool) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok || unixConn == nil {
		return -1, false
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return -1, false
	}
	var ucred *unix.Ucred
	var getErr error
	err = raw.Control(func(fd uintptr) {
		ucred, getErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil || getErr != nil || ucred == nil {
		return -1, false
	}
	return int(ucred.Uid), true
}
