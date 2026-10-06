package unixsock

import (
	"net"
	"testing"
)

func TestPeerUIDNonUNIX(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() {
		_ = a.Close()
		_ = b.Close()
	})
	uid, ok := PeerUID(a)
	if ok || uid != -1 {
		t.Fatalf("PeerUID(pipe) = (%d, %v), want (-1, false)", uid, ok)
	}
	uid, ok = PeerUID(nil)
	if ok || uid != -1 {
		t.Fatalf("PeerUID(nil) = (%d, %v), want (-1, false)", uid, ok)
	}
}
