//go:build !linux && !darwin

package api

import "syscall"

func peerUIDFromRawConn(_ syscall.RawConn) (int, int, bool) {
	return 0, 0, false
}
