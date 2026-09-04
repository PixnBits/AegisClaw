//go:build testunixgit

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestGitUnixLookupWithTestunixgit(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	pubA, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubB, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubAStr := base64.StdEncoding.EncodeToString(pubA)
	pubBStr := base64.StdEncoding.EncodeToString(pubB)
	dir := t.TempDir()
	identPath := filepath.Join(dir, "git-identities.json")
	identJSON, err := json.Marshal(map[string]string{pubAStr: "tenant-a", pubBStr: "tenant-b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identPath, identJSON, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_GIT_IDENTITIES", identPath)
	unixAddr := &net.UnixAddr{Name: "hub.sock", Net: "unix"}
	got, err := tenantForGit(pubAStr, unixAddr)
	if err != nil || got != "tenant-a" {
		t.Fatalf("unix testunixgit pubA: tenant=%q err=%v, want tenant-a", got, err)
	}
	got, err = tenantForGit(pubBStr, unixAddr)
	if err != nil || got != "tenant-b" {
		t.Fatalf("unix testunixgit pubB: tenant=%q err=%v, want tenant-b", got, err)
	}
}
