package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PixnBits/AegisClaw/internal/dashboard"
	"golang.org/x/sys/unix"
)

const (
	portalVsockPort = 18080
	hostAPIPort     = 1030
)

type bridgeRequest struct {
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type portalAPIClient struct{}

func (c *portalAPIClient) Call(ctx context.Context, action string, payload json.RawMessage) (*dashboard.APIResponse, error) {
	conn, err := dialHostAPIBridge(ctx, hostAPIPort)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req := bridgeRequest{Action: action, Payload: payload}
	if err := json.NewEncoder(conn).Encode(&req); err != nil {
		return nil, fmt.Errorf("encode bridge request: %w", err)
	}

	var resp dashboard.APIResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode bridge response: %w", err)
	}
	return &resp, nil
}

func main() {
	mountEssentialFS()

	listener, err := listenVsock(portalVsockPort)
	if err != nil {
		log.Fatalf("portal: listen vsock: %v", err)
	}
	defer listener.Close()

	dash, err := dashboard.New("vsock", &portalAPIClient{})
	if err != nil {
		log.Fatalf("portal: init dashboard: %v", err)
	}

	httpSrv := &http.Server{Handler: dash}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdownCtx) //nolint:errcheck
	}()

	log.Printf("portal: listening on vsock://:%d", portalVsockPort)
	if err := httpSrv.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatalf("portal: serve: %v", err)
	}
}

func dialHostAPIBridge(ctx context.Context, port uint32) (net.Conn, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("vsock socket: %w", err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			unix.Close(fd)
			return nil, fmt.Errorf("context already expired")
		}
		tv := unix.NsecToTimeval(remaining.Nanoseconds())
		_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_SNDTIMEO, &tv)
		_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
	}

	sa := &unix.SockaddrVM{CID: unix.VMADDR_CID_HOST, Port: port}
	if err := unix.Connect(fd, sa); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("vsock connect host api bridge: %w", err)
	}

	file := os.NewFile(uintptr(fd), "portal-host-api")
	return &vsockConn{file: file}, nil
}

func mountEssentialFS() {
	mounts := []struct {
		source string
		target string
		fstype string
		flags  uintptr
	}{
		{"proc", "/proc", "proc", 0},
		{"sysfs", "/sys", "sysfs", syscall.MS_RDONLY},
		{"devtmpfs", "/dev", "devtmpfs", 0},
		{"tmpfs", "/tmp", "tmpfs", 0},
		{"tmpfs", "/run", "tmpfs", 0},
	}

	for _, m := range mounts {
		_ = os.MkdirAll(m.target, 0755)
		if err := syscall.Mount(m.source, m.target, m.fstype, m.flags, ""); err != nil {
			log.Printf("warning: mount %s on %s failed: %v", m.fstype, m.target, err)
		}
	}
}

func listenVsock(port uint32) (net.Listener, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("socket(AF_VSOCK): %w", err)
	}

	sa := &unix.SockaddrVM{CID: unix.VMADDR_CID_ANY, Port: port}
	if err := unix.Bind(fd, sa); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("bind vsock port %d: %w", port, err)
	}
	if err := unix.Listen(fd, 16); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("listen vsock: %w", err)
	}
	return &vsockListener{fd: fd, port: int(port)}, nil
}

type vsockConn struct {
	file *os.File
}

func (c *vsockConn) Read(b []byte) (int, error)         { return c.file.Read(b) }
func (c *vsockConn) Write(b []byte) (int, error)        { return c.file.Write(b) }
func (c *vsockConn) Close() error                       { return c.file.Close() }
func (c *vsockConn) LocalAddr() net.Addr                { return vsockAddr(0) }
func (c *vsockConn) RemoteAddr() net.Addr               { return vsockAddr(0) }
func (c *vsockConn) SetDeadline(t time.Time) error      { return c.file.SetDeadline(t) }
func (c *vsockConn) SetReadDeadline(t time.Time) error  { return c.file.SetReadDeadline(t) }
func (c *vsockConn) SetWriteDeadline(t time.Time) error { return c.file.SetWriteDeadline(t) }

type vsockListener struct {
	fd   int
	port int
}

func (l *vsockListener) Accept() (net.Conn, error) {
	nfd, _, err := unix.Accept(l.fd)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(nfd), "portal-vsock")
	return &vsockConn{file: file}, nil
}

func (l *vsockListener) Close() error   { return unix.Close(l.fd) }
func (l *vsockListener) Addr() net.Addr { return vsockAddr(l.port) }

type vsockAddr int

func (a vsockAddr) Network() string { return "vsock" }
func (a vsockAddr) String() string  { return fmt.Sprintf("vsock://:%d", int(a)) }
