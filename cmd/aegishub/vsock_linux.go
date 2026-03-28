package main

import (
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// listenAFVsock opens an AF_VSOCK server socket bound to the given port.
// VMADDR_CID_ANY lets the guest accept connections from any CID; the host
// daemon will connect with CID 2 (VMADDR_CID_HOST).
func listenAFVsock(port uint32) (net.Listener, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("AF_VSOCK socket: %w", err)
	}

	sa := &unix.SockaddrVM{
		CID:  unix.VMADDR_CID_ANY,
		Port: port,
	}
	if err := unix.Bind(fd, sa); err != nil {
		unix.Close(fd) //nolint:errcheck
		return nil, fmt.Errorf("AF_VSOCK bind port %d: %w", port, err)
	}

	if err := unix.Listen(fd, unix.SOMAXCONN); err != nil {
		unix.Close(fd) //nolint:errcheck
		return nil, fmt.Errorf("AF_VSOCK listen: %w", err)
	}

	return &vsockListener{fd: fd, port: port}, nil
}

// vsockListener implements net.Listener over a raw AF_VSOCK file descriptor.
// Go's net.FileListener cannot handle AF_VSOCK (getsockname returns an address
// family the net package doesn't understand), so we wrap the fd manually.
type vsockListener struct {
	fd   int
	port uint32
}

func (l *vsockListener) Accept() (net.Conn, error) {
	nfd, _, err := unix.Accept(l.fd)
	if err != nil {
		return nil, err
	}
	return &vsockConn{file: os.NewFile(uintptr(nfd), "vsock-conn")}, nil
}

func (l *vsockListener) Close() error   { return unix.Close(l.fd) }
func (l *vsockListener) Addr() net.Addr { return vsockAddr(l.port) }

// vsockAddr implements net.Addr for AF_VSOCK.
type vsockAddr uint32

func (a vsockAddr) Network() string { return "vsock" }
func (a vsockAddr) String() string  { return fmt.Sprintf("vsock://:%d", uint32(a)) }

// vsockConn wraps an os.File over an AF_VSOCK fd as a net.Conn.
type vsockConn struct{ file *os.File }

func (c *vsockConn) Read(b []byte) (int, error)  { return c.file.Read(b) }
func (c *vsockConn) Write(b []byte) (int, error) { return c.file.Write(b) }
func (c *vsockConn) Close() error                { return c.file.Close() }
func (c *vsockConn) LocalAddr() net.Addr         { return vsockAddr(0) }
func (c *vsockConn) RemoteAddr() net.Addr        { return vsockAddr(0) }

func (c *vsockConn) SetDeadline(t time.Time) error {
	return c.file.SetDeadline(t)
}
func (c *vsockConn) SetReadDeadline(t time.Time) error {
	return c.file.SetReadDeadline(t)
}
func (c *vsockConn) SetWriteDeadline(t time.Time) error {
	return c.file.SetWriteDeadline(t)
}
