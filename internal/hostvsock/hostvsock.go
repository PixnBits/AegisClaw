// Package hostvsock is the only way a host binary (cmd/aegis, cmd/aegishub)
// may accept AF_VSOCK connections. Every accepted peer must pass a guest CID
// check and the caller's allow func; the rest are closed before the caller
// sees them. TestHostBinariesUseGatedVsock enforces this.
//
// Firecracker guests never reach a host AF_VSOCK listener (Firecracker
// exposes guest vsock as host UNIX sockets), so host binaries have no
// listener today. This gate exists for any future one.
package hostvsock

import (
	"net"

	"github.com/mdlayher/vsock"
)

// IsGuestCID reports a CID a guest VM can have. 0 (hypervisor), 1 (local),
// 2 (host) and VMADDR_CID_ANY are never guests.
func IsGuestCID(cid uint32) bool {
	return cid > 2 && cid != ^uint32(0)
}

// Admit reports whether addr is a vsock peer with a guest CID that allow
// accepts. allow must be backed by daemon state, never by the environment.
func Admit(addr net.Addr, allow func(cid uint32) bool) bool {
	a, ok := addr.(*vsock.Addr)
	if !ok || a == nil || !IsGuestCID(a.ContextID) {
		return false
	}
	return allow != nil && allow(a.ContextID)
}

type gatedListener struct {
	net.Listener
	allow func(cid uint32) bool
}

// Accept returns the next admitted connection and closes refused ones.
func (l *gatedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if Admit(c.RemoteAddr(), l.allow) {
			return c, nil
		}
		_ = c.Close()
	}
}

// Gate wraps a listener so Accept only returns admitted peers.
func Gate(l net.Listener, allow func(cid uint32) bool) net.Listener {
	return &gatedListener{Listener: l, allow: allow}
}

// Listen opens an AF_VSOCK listener on port that only admits guest CIDs
// accepted by allow.
func Listen(port uint32, allow func(cid uint32) bool) (net.Listener, error) {
	l, err := vsock.Listen(port, nil)
	if err != nil {
		return nil, err
	}
	return Gate(l, allow), nil
}
