package hublease

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUnleaseCIDPoisonsSamePub(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	const cid uint32 = 7
	StoreLease(cid, "pub-a")
	got, ok := LoadLease(cid)
	if !ok || got != "pub-a" {
		t.Fatalf("lease: got %q ok=%v", got, ok)
	}
	if !UnleaseCID(cid, "pub-a") {
		t.Fatal("CAS unlease of matching pub must succeed")
	}
	if _, ok := LoadLease(cid); ok {
		t.Fatal("after UnleaseCID, in-memory lease must be gone")
	}
	closed, ok := ClosedPub(cid)
	if !ok || closed != "pub-a" {
		t.Fatalf("poison: got %q ok=%v, want pub-a", closed, ok)
	}
}

func TestStoreLeaseClearsPoison(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	const cid uint32 = 9
	StoreLease(cid, "pub-a")
	UnleaseCID(cid, "pub-a")
	StoreLease(cid, "pub-a")
	got, ok := LoadLease(cid)
	if !ok || got != "pub-a" {
		t.Fatalf("re-lease after UnleaseCID: got %q ok=%v", got, ok)
	}
	if closed, ok := ClosedPub(cid); ok {
		t.Fatalf("StoreLease must clear poison, still %q", closed)
	}
}

func TestUnleaseCIDCASSkipsMismatchedPub(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	const cid uint32 = 42
	StoreLease(cid, "pub-a")
	StoreLease(cid, "pub-b") // B reused CID 42
	if UnleaseCID(cid, "pub-a") {
		t.Fatal("late unlease of A must not CAS-succeed after B reused the CID")
	}
	got, ok := LoadLease(cid)
	if !ok || got != "pub-b" {
		t.Fatalf("B must keep lease: got %q ok=%v", got, ok)
	}
	if closed, ok := ClosedPub(cid); ok {
		t.Fatalf("CAS miss must not poison B, closed=%q", closed)
	}
}

func TestUnleaseCIDEmptyExpectedPubDoesNotBlindUnlease(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	const cid uint32 = 5
	StoreLease(cid, "pub-a")
	if UnleaseCID(cid, "") {
		t.Fatal("empty expectedPub must not blind-unlease")
	}
	if UnleaseCID(cid, "   ") {
		t.Fatal("whitespace expectedPub must not blind-unlease")
	}
	got, ok := LoadLease(cid)
	if !ok || got != "pub-a" {
		t.Fatalf("lease must remain: got %q ok=%v", got, ok)
	}
}

func TestDeleteCIDKeyIfCAS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cid.json")
	if err := os.WriteFile(path, []byte(`{"42":"pub-a","7":"keep"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if DeleteCIDKeyIf(path, 42, "pub-b") {
		t.Fatal("mismatch must not delete")
	}
	if !DeleteCIDKeyIf(path, 42, "pub-a") {
		t.Fatal("matching pub must delete")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["42"]; ok {
		t.Fatalf("row 42 must be gone: %s", b)
	}
	if m["7"] != "keep" {
		t.Fatalf("row 7 must remain: %s", b)
	}
}
