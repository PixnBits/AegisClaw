package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadMerkleHashAtIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kernel.merkle.jsonl")
	content := []byte(`{"hash":"aaa111"}
{"hash":"bbb222"}
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readMerkleHashAtIndex(path, 2)
	if err != nil {
		t.Fatalf("readMerkleHashAtIndex: %v", err)
	}
	if got != "bbb222" {
		t.Fatalf("got %q, want %q", got, "bbb222")
	}
}

func TestReadMerkleHashAtIndexIgnoresMalformedTailPastRequestedIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kernel.merkle.jsonl")
	content := []byte(`{"hash":"aaa111"}
{"hash":"bbb222"}
{not-json
`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readMerkleHashAtIndex(path, 2)
	if err != nil {
		t.Fatalf("readMerkleHashAtIndex should not parse tail after requested index: %v", err)
	}
	if got != "bbb222" {
		t.Fatalf("got %q, want %q", got, "bbb222")
	}
}
