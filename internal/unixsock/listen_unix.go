//go:build unix

package unixsock

import (
	"net"
	"sync"
	"syscall"
)

// umaskMu is the only process-wide umask lock. Umask applies to every
// thread, so the hub and the daemon must take this same mutex.
var umaskMu sync.Mutex

// ListenPrivate listens on network/addr while the process umask is 0177,
// so a unix socket is created at 0600 (0777 &^ 0177). The previous umask
// is restored before return.
func ListenPrivate(network, addr string) (net.Listener, error) {
	umaskMu.Lock()
	defer umaskMu.Unlock()
	old := syscall.Umask(0177)
	defer syscall.Umask(old)
	return net.Listen(network, addr)
}
