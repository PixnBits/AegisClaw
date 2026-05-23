//go:build linux

package api

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func peerUIDFromRawConn(raw syscall.RawConn) (int, int, bool) {
	var uid int
	var pid int
	var okUID bool
	_ = raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err == nil {
			uid = int(cred.Uid)
			pid = int(cred.Pid)
			okUID = true
		}
	})
	return uid, pid, okUID
}
