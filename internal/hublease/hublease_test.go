package hublease

import "testing"

func TestUnleaseCIDPoisonsSamePub(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	const cid uint32 = 7
	StoreLease(cid, "pub-a")
	got, ok := LoadLease(cid)
	if !ok || got != "pub-a" {
		t.Fatalf("lease: got %q ok=%v", got, ok)
	}
	UnleaseCID(cid)
	if _, ok := LoadLease(cid); ok {
		t.Fatal("after UnleaseCID, in-memory lease must be gone")
	}
	closed, ok := ClosedPub(cid)
	if !ok || closed != "pub-a" {
		t.Fatalf("poison: got %q ok=%v, want pub-a", closed, ok)
	}
}
