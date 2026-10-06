//go:build linux

package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFirecrackerStartRejectsUnsafeIDBeforeFiles(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "a", "b", "c", "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	fb := NewFirecrackerBackend(state)
	for _, id := range []string{"../../../x", "../x", "Coder-1", "foo--bar", "coder-"} {
		if err := ValidateVMID(id); err == nil {
			t.Fatalf("ValidateVMID(%q) accepted; not calling Start", id)
		}
		err := fb.Start(context.Background(), VMConfig{ID: id})
		if !errors.Is(err, ErrInvalidVMID) {
			t.Fatalf("Start(%q) err=%v", id, err)
		}
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			t.Errorf("file created: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
