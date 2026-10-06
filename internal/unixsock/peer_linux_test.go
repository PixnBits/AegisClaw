//go:build linux

package unixsock

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPeerUIDUnixSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "peer.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	accepted := make(chan net.Conn, 1)
	errc := make(chan error, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			errc <- aerr
			return
		}
		accepted <- c
	}()

	client, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var server net.Conn
	select {
	case err := <-errc:
		t.Fatal(err)
	case server = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("accept timed out")
	}
	t.Cleanup(func() { _ = server.Close() })

	uid, ok := PeerUID(server)
	if !ok {
		t.Fatal("PeerUID failed on a real unix connection")
	}
	if uid != os.Geteuid() {
		t.Fatalf("PeerUID = %d, want euid %d", uid, os.Geteuid())
	}

	// File() would have cleared O_NONBLOCK. A short deadline must fire.
	errc = make(chan error, 1)
	go func() {
		if err := server.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
			errc <- err
			return
		}
		var buf [1]byte
		_, err := server.Read(buf[:])
		errc <- err
	}()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("Read succeeded, want timeout")
		}
		var nerr net.Error
		if !errors.Is(err, os.ErrDeadlineExceeded) && !(errors.As(err, &nerr) && nerr.Timeout()) {
			t.Fatalf("Read err = %v, want deadline exceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Read blocked for >1s after PeerUID; socket is blocking")
	}
}

func TestPeerUIDClosedUnixConn(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "peer-closed.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	accepted := make(chan net.Conn, 1)
	errc := make(chan error, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			errc <- aerr
			return
		}
		accepted <- c
	}()

	client, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var server net.Conn
	select {
	case err := <-errc:
		t.Fatal(err)
	case server = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("accept timed out")
	}
	unixConn, ok := server.(*net.UnixConn)
	if !ok {
		t.Fatalf("accepted %T, want *net.UnixConn", server)
	}
	if err := unixConn.Close(); err != nil {
		t.Fatal(err)
	}
	uid, ok := PeerUID(unixConn)
	if ok || uid != -1 {
		t.Fatalf("PeerUID(closed) = (%d, %v), want (-1, false)", uid, ok)
	}
}
