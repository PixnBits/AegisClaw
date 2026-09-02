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
	template := filepath.Join(templateDir, "shared-rootfs-template.img")
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

	// Plant leftover git/creds ON the private image (bytes stand in for guest
	// files inside the image) and as siblings Stop must remove.
	poison := []byte("https://builder:tok@example.test\n")
	if err := os.WriteFile(wantPrivate, poison, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".git-credentials", ".netrc", "id_rsa", "id_ed25519"} {
		if err := os.WriteFile(filepath.Join(stateDir, name), poison, 0600); err != nil {
			t.Fatal(err)
		}
	}
	work := filepath.Join(stateDir, "builder-work")
	if err := os.MkdirAll(work, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "secret"), poison, 0600); err != nil {
		t.Fatal(err)
	}

	fb := NewFirecrackerBackend(stateDir)
	fb.mu.Lock()
	fb.vms[vmID] = &firecrackerVM{
		config: VMConfig{
			ID:         vmID,
			Image:      "builder.img",
			RootfsPath: template, // shared template — Stop must not delete/truncate it
		},
		sockPath: filepath.Join(stateDir, "fc-"+vmID+".sock"),
	}
	fb.mu.Unlock()

	if err := fb.Stop(context.Background(), vmID); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if _, err := os.Stat(wantPrivate); !os.IsNotExist(err) {
		t.Fatalf("private rootfs.img must be gone after Stop, stat err=%v", err)
	}
	for _, name := range []string{".git-credentials", ".netrc", "id_rsa", "id_ed25519", "builder-work"} {
		if _, err := os.Stat(filepath.Join(stateDir, name)); !os.IsNotExist(err) {
			t.Fatalf("planted leftover %s must be gone after Stop, stat err=%v", name, err)
		}
	}

	got, err := os.ReadFile(template)
	if err != nil {
		t.Fatalf("shared template must still exist: %v", err)
	}
	if !bytes.Equal(got, templateBytes) {
		t.Fatalf("shared template poisoned or truncated: got %q want %q", got, templateBytes)
	}
}
