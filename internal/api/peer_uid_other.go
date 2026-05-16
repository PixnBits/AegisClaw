//go:build !linux && !darwin

package api

import "syscall"

func peerUIDFromRawConn(_ syscall.RawConn) (int, bool) {
	return 0, false
}
