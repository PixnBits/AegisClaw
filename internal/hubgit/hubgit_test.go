package hubgit

import (
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeMissingSessionDeniesWithoutDial(t *testing.T) {
	store := mustListen(t)
	defer store.ln.Close()
	go acceptNone(t, store.ln)

	c1, c2 := net.Pipe()
	defer c1.Close()
	go Serve(c2, "", store.path)
	mustDeny(t, c1)
}

func TestServeMismatchDeniesWithoutDial(t *testing.T) {
	store := mustListen(t)
	defer store.ln.Close()
	go acceptNone(t, store.ln)

	c1, c2 := net.Pipe()
	defer c1.Close()
	go Serve(c2, "tenant-a", store.path)
	_, _ = fmt.Fprintf(c1, "git-connect git-upload-pack hub::vsock/tenant-b/skill\n")
	line := readLine(t, c1)
	if !strings.Contains(line, "not your tenant") {
		t.Fatalf("want tenancy deny, got %q", line)
	}
}

func TestServeMatchSplicesSessionTenantNotURL(t *testing.T) {
	store := mustListen(t)
	defer store.ln.Close()
	headerCh := make(chan string, 1)
	go func() {
		c, err := store.ln.Accept()
		if err != nil {
			headerCh <- "accept: " + err.Error()
			return
		}
		defer c.Close()
		buf := make([]byte, 256)
		n, _ := c.Read(buf)
		headerCh <- string(buf[:n])
		_, _ = c.Write([]byte("pack-ok"))
	}()

	c1, c2 := net.Pipe()
	defer c1.Close()
	go Serve(c2, "tenant-a", store.path)
	_, _ = fmt.Fprintf(c1, "git-connect git-receive-pack hub::vsock/tenant-a/skill\n")
	reply := readLine(t, c1)
	if reply != "ok" {
		t.Fatalf("want ok, got %q", reply)
	}
	select {
	case h := <-headerCh:
		if !strings.HasPrefix(h, "git-receive-pack tenant-a skill\n") {
			t.Fatalf("Store header must be session tenant, not URL: %q", h)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Store was not dialed")
	}
}

type sock struct {
	ln   net.Listener
	path string
}

func mustListen(t *testing.T) sock {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "private.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	return sock{ln: ln, path: path}
}

func acceptNone(t *testing.T, ln net.Listener) {
	t.Helper()
	if ul, ok := ln.(*net.UnixListener); ok {
		_ = ul.SetDeadline(time.Now().Add(500 * time.Millisecond))
	}
	c, err := ln.Accept()
	if err == nil {
		_ = c.Close()
		t.Errorf("Store was dialed on deny path")
	}
}

func mustDeny(t *testing.T, c net.Conn) {
	t.Helper()
	_, _ = fmt.Fprintf(c, "git-connect git-upload-pack hub::vsock/tenant-a/skill\n")
	line := readLine(t, c)
	if !strings.Contains(line, "not your tenant") {
		t.Fatalf("want tenancy deny, got %q", line)
	}
}

func readLine(t *testing.T, c net.Conn) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 256)
	n, err := c.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read: %v", err)
	}
	return strings.TrimSpace(string(buf[:n]))
}
