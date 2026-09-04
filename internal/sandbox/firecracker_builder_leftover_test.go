//go:build linux

package sandbox

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFirecrackerStopWipesBuilderLeftoverRootfs(t *testing.T) {
	stateDir := t.TempDir()
	templateDir := t.TempDir()
	template := filepath.Join(templateDir, "builder.img")
	templateBytes := []byte("TEMPLATE-UNPOISONED-ROOTFS-BYTES")
	if err := os.WriteFile(template, templateBytes, 0644); err != nil {
		t.Fatal(err)
	}

	const vmID = "builder-sit-1"
	private := prepareVMRootfs(stateDir, vmID, template, "")
	wantPrivate := filepath.Join(stateDir, vmID+".rootfs.img")
	if private == template {
		t.Fatal("builder must get a private rootfs copy, never the shared template")
	}
	if private != wantPrivate {
		t.Fatalf("private rootfs path = %s, want %s", private, wantPrivate)
	}
	if _, err := os.Stat(wantPrivate); err != nil {
		t.Fatalf("private builder-sit-1.rootfs.img missing after prepareVMRootfs: %v", err)
	}

	// Plant leftover git/creds INSIDE the private image (bytes in the img file
	// stand in for guest files). Do not mount a live guest; no KVM.
	poison := []byte("https://builder:tok@example.test\n")
	if err := os.WriteFile(wantPrivate, poison, 0600); err != nil {
		t.Fatal(err)
	}

	fb := NewFirecrackerBackend(stateDir)
	fb.mu.Lock()
	fb.vms[vmID] = &firecrackerVM{
		config: VMConfig{
			ID:         vmID,
			Image:      "builder.img",
			RootfsPath: template,
		},
		sockPath: filepath.Join(stateDir, "fc-"+vmID+".sock"),
	}
	fb.mu.Unlock()

	if err := fb.Stop(context.Background(), vmID); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if _, err := os.Stat(wantPrivate); !os.IsNotExist(err) {
		t.Fatalf("private rootfs.img must be unlinked after Stop, stat err=%v", err)
	}

	got, err := os.ReadFile(template)
	if err != nil {
		t.Fatalf("shared template must still exist: %v", err)
	}
	if bytes.Contains(got, poison) {
		t.Fatal("shared template must not contain the plant")
	}
	if !bytes.Equal(got, templateBytes) {
		t.Fatalf("shared template poisoned or truncated: got %q want %q", got, templateBytes)
	}
}
