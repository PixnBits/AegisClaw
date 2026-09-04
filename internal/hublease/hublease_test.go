package hublease

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUnleaseCIDDeletesLease(t *testing.T) {
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
	if !CASFillLease(cid, "pub-a") {
		t.Fatal("same pub may fill empty slot after UnleaseCID")
	}
	got, ok = LoadLease(cid)
	if !ok || got != "pub-a" {
		t.Fatalf("refill same pub: got %q ok=%v", got, ok)
	}
}

func TestCASFillLeaseAfterUnleaseFillsEmptySlot(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	const cid uint32 = 9
	StoreLease(cid, "pub-a")
	if !UnleaseCID(cid, "pub-a") {
		t.Fatal("unlease")
	}
	if _, ok := LoadLease(cid); ok {
		t.Fatal("git-connect LoadLease after unlease and before handshake must be empty")
	}
	if !CASFillLease(cid, "pub-b") {
		t.Fatal("new occupant pub may fill because the slot is empty")
	}
	got, ok := LoadLease(cid)
	if !ok || got != "pub-b" {
		t.Fatalf("new pub fill: got %q ok=%v", got, ok)
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

func TestStoreLeaseCASEmptyOrSame(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	const cid uint32 = 11
	if !StoreLeaseCAS(cid, "pub-a") {
		t.Fatal("empty lease must CAS-store")
	}
	got, ok := LoadLease(cid)
	if !ok || got != "pub-a" {
		t.Fatalf("after empty CAS: got %q ok=%v", got, ok)
	}
	if !StoreLeaseCAS(cid, "pub-a") {
		t.Fatal("same pub must CAS-succeed")
	}
	if StoreLeaseCAS(cid, "pub-b") {
		t.Fatal("different pub must not overwrite")
	}
	got, ok = LoadLease(cid)
	if !ok || got != "pub-a" {
		t.Fatalf("mismatch must keep A: got %q ok=%v", got, ok)
	}
}

func TestStoreLeaseCASAfterUnleaseFillsEmpty(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	const cid uint32 = 12
	StoreLease(cid, "pub-a")
	UnleaseCID(cid, "pub-a")
	if !StoreLeaseCAS(cid, "pub-a") {
		t.Fatal("CAS-store of same pub after unlease must succeed because the slot is empty")
	}
	got, ok := LoadLease(cid)
	if !ok || got != "pub-a" {
		t.Fatalf("re-lease: got %q ok=%v", got, ok)
	}
}

func TestStoreLeaseIfAbsentOrSameEmptyOrSame(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	const cid uint32 = 11
	if StoreLeaseIfAbsentOrSame(cid, "") || StoreLeaseIfAbsentOrSame(0, "pub-a") {
		t.Fatal("empty pub or CID 0 must not fill")
	}
	if !StoreLeaseIfAbsentOrSame(cid, "pub-a") {
		t.Fatal("empty slot must CAS-fill")
	}
	got, ok := LoadLease(cid)
	if !ok || got != "pub-a" {
		t.Fatalf("fill: got %q ok=%v", got, ok)
	}
	if !StoreLeaseIfAbsentOrSame(cid, "pub-a") {
		t.Fatal("same pub must CAS-succeed")
	}
}

func TestStoreLeaseIfAbsentOrSameNeverOverwritesDifferentPub(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	const cid uint32 = 12
	if !StoreLeaseIfAbsentOrSame(cid, "pub-a") {
		t.Fatal("first fill")
	}
	if StoreLeaseIfAbsentOrSame(cid, "pub-b") {
		t.Fatal("second guest different pub must not overwrite")
	}
	got, ok := LoadLease(cid)
	if !ok || got != "pub-a" {
		t.Fatalf("A must remain: got %q ok=%v", got, ok)
	}
}

func TestStoreLeaseIfAbsentOrSameFillsAfterUnlease(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	const cid uint32 = 13
	StoreLease(cid, "pub-a")
	if !UnleaseCID(cid, "pub-a") {
		t.Fatal("unlease")
	}
	if _, ok := LoadLease(cid); ok {
		t.Fatal("LoadLease after unlease must be empty")
	}
	if !StoreLeaseIfAbsentOrSame(cid, "pub-a") {
		t.Fatal("handshake may fill empty slot after StopVM")
	}
	got, ok := LoadLease(cid)
	if !ok || got != "pub-a" {
		t.Fatalf("fill after unlease: got %q ok=%v", got, ok)
	}
}
