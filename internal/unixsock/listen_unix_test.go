//go:build unix

package unixsock

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestListenPrivateModeAndUmask(t *testing.T) {
	orig := syscall.Umask(0)
	t.Cleanup(func() { _ = syscall.Umask(orig) })

	sock := filepath.Join(t.TempDir(), "priv.sock")
	ln, err := ListenPrivate("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	fi, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %o, want 0600 (umask was 0)", fi.Mode().Perm())
	}
	if _, ok := ln.(*net.UnixListener); !ok {
		t.Fatalf("listener type %T, want *net.UnixListener", ln)
	}

	got := syscall.Umask(0)
	if got != 0 {
		t.Fatalf("umask = %#o after ListenPrivate, want 0 restored", got)
	}
}
