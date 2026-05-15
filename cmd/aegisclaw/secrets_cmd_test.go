package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestReadSecretPayloadStdin(t *testing.T) {
	cmd := &cobra.Command{}
	registerSecretsInputFlags(cmd)
	secretsFromStdin = true
	t.Cleanup(func() { secretsFromStdin = false })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })

	go func() {
		_, _ = w.WriteString("hunter2\n")
		w.Close()
	}()

	val, err := readSecretPayload(cmd, "ignored: ")
	if err != nil {
		t.Fatal(err)
	}
	if val != "hunter2" {
		t.Fatalf("got %q", val)
	}
}

func TestReadSecretPayloadFileNoFollow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(path, []byte("file-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	secretsFromFile = path
	t.Cleanup(func() { secretsFromFile = "" })

	val, err := readSecretPayload(cmd, "")
	if err != nil {
		t.Fatal(err)
	}
	if val != "file-secret" {
		t.Fatalf("got %q", val)
	}
}

func TestReadSecretPayloadFileRejectsSymlinkOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only O_NOFOLLOW behavior")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	secretsFromFile = link
	t.Cleanup(func() { secretsFromFile = "" })
	if _, err := readSecretPayload(cmd, ""); err == nil {
		t.Fatal("expected symlink input to be rejected on linux")
	}
}

func TestReadSecretPayloadRejectsBothStdinAndFile(t *testing.T) {
	cmd := &cobra.Command{}
	secretsFromStdin = true
	secretsFromFile = "/tmp/x"
	t.Cleanup(func() {
		secretsFromStdin = false
		secretsFromFile = ""
	})
	_, err := readSecretPayload(cmd, "")
	if err == nil || !strings.Contains(err.Error(), "one of") {
		t.Fatalf("expected mutual exclusion error, got %v", err)
	}
}

func TestReadSecretPayloadStdinTooLarge(t *testing.T) {
	cmd := &cobra.Command{}
	registerSecretsInputFlags(cmd)
	secretsFromStdin = true
	t.Cleanup(func() { secretsFromStdin = false })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })

	go func() {
		_, _ = w.WriteString(strings.Repeat("a", maxSecretFileBytes+1))
		w.Close()
	}()

	_, err = readSecretPayload(cmd, "ignored: ")
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected size error, got %v", err)
	}
}
