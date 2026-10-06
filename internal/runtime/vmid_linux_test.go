//go:build linux

package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"AegisClaw/internal/config"
	"AegisClaw/internal/sandbox"
)

// TestTraversalIDsCreateNoStateFiles points StateDir at a directory named
// state under a few parents. The parents keep a three-level ".." id inside
// the temp root if a check is missing; the walk still requires zero files
// inside or outside state.
func TestTraversalIDsCreateNoStateFiles(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "a", "b", "c", "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	backend := sandbox.NewFirecrackerBackend(state)
	o := &Orchestrator{
		config:  &config.Config{StateDir: state, SandboxType: config.Firecracker},
		backend: backend,
		vms:     map[string]*VMLifecycle{},
	}

	roles := []struct{ role, channel string }{
		{"../x", ""},
		{"coder", "../../../x"},
		{"coder", "../x"},
		{"agent", "../../../x"},
		{"", "../../../x"},
		{"store", "../../../x"},
	}
	for _, tc := range roles {
		if _, err := o.EnsureRoleAgent(context.Background(), tc.role, tc.channel); !EnsureRoleRefused(err) {
			t.Fatalf("EnsureRoleAgent(%q, %q) err=%v", tc.role, tc.channel, err)
		}
	}
	for _, id := range []string{"../../../x", "../x", "coder/../../../x", "Coder-1"} {
		if err := ValidateVMID(id); err == nil {
			t.Fatalf("ValidateVMID(%q) accepted; not calling writers", id)
		}
		if err := o.StartVM(context.Background(), "agent", id, "agent.img"); !errors.Is(err, ErrInvalidVMID) {
			t.Fatalf("StartVM(%q) err=%v", id, err)
		}
		if err := backend.Start(context.Background(), sandbox.VMConfig{ID: id}); !errors.Is(err, sandbox.ErrInvalidVMID) {
			t.Fatalf("sandbox Start(%q) err=%v", id, err)
		}
	}

	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("files created under %s: %v", root, files)
	}
}
