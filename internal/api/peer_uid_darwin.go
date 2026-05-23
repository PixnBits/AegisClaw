//go:build darwin

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
		cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if err == nil && cred != nil {
			uid = int(cred.Uid)
			okUID = true
		}
	})
	return uid, pid, okUID
}
