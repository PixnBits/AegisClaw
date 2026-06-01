package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_BasicPrecedence(t *testing.T) {
	tmp := t.TempDir()

	// Create root-level file (highest precedence)
	err := os.WriteFile(filepath.Join(tmp, "SOUL.md"), []byte("root-soul"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create structured default (lower)
	err = os.MkdirAll(filepath.Join(tmp, "agents", "default"), 0755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(tmp, "agents", "default", "SOUL.md"), []byte("default-soul"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	ctx, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if ctx.SOUL != "root-soul" {
		t.Errorf("expected root-level to win, got %q", ctx.SOUL)
	}
}

func TestLoad_Security_PathTraversal(t *testing.T) {
	tmp := t.TempDir()

	// Attempt to write outside
	badPath := filepath.Join(tmp, "..", "evil.md")
	// We test that Load refuses to read it even if the file exists
	_ = os.WriteFile(badPath, []byte("evil"), 0644) // may fail, that's ok

	ctx, err := Load(tmp)
	if err != nil {
		// Load on empty dir is fine
		return
	}
	// Even if somehow loaded, the content must never come from outside
	if strings.Contains(ctx.SOUL, "evil") {
		t.Error("path traversal succeeded - security failure")
	}
	_ = ctx
}

func TestLoad_Security_SizeLimit(t *testing.T) {
	tmp := t.TempDir()

	// Create an oversized file
	big := make([]byte, 200*1024) // > 128 KiB
	for i := range big {
		big[i] = 'A'
	}
	err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), big, 0644)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Load(tmp)
	if err == nil || !strings.Contains(err.Error(), "exceeds size limit") {
		t.Errorf("expected size limit error, got: %v", err)
	}
}

func TestLoad_Security_UnsafePermissions(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "SOUL.md")

	err := os.WriteFile(path, []byte("secret"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Use chmod to force an unsafe mode independent of process umask.
	err = os.Chmod(path, 0777) // world-writable
	if err != nil {
		t.Fatal(err)
	}

	_, err = Load(tmp)
	if err == nil || !strings.Contains(err.Error(), "unsafe file permissions") {
		t.Errorf("expected unsafe permissions error, got: %v", err)
	}
}

func TestLoad_Security_DangerousContent(t *testing.T) {
	tmp := t.TempDir()

	dangerous := "#!/bin/bash\necho pwned\nexec something"
	err := os.WriteFile(filepath.Join(tmp, "AGENTS.md"), []byte(dangerous), 0644)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Load(tmp)
	if err == nil || !strings.Contains(err.Error(), "potentially executable") {
		t.Errorf("expected dangerous content rejection, got: %v", err)
	}
}

func TestLoad_MissingFilesOK(t *testing.T) {
	tmp := t.TempDir()
	// Empty directory should load cleanly with empty context
	ctx, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load on empty dir should succeed: %v", err)
	}
	if ctx.SOUL != "" || ctx.AGENTS != "" {
		t.Error("expected empty context for missing files")
	}
}
