//go:build !unix

package unixsock

import (
	"errors"
	"net"
)

// ListenPrivate is unavailable where unix sockets and Umask are not.
func ListenPrivate(network, addr string) (net.Listener, error) {
	return nil, errors.New("unixsock: ListenPrivate is not supported for " + network + " " + addr)
}
