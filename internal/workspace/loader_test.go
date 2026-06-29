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

func TestLoadForAgent_SettingsPrecedenceAndValidate(t *testing.T) {
	tmp := t.TempDir()
	// default
	os.MkdirAll(filepath.Join(tmp, "agents", "default"), 0755)
	os.WriteFile(filepath.Join(tmp, "agents", "default", "SETTINGS.yaml"), []byte("model: default-model\nautonomy_level: 1\n"), 0644)
	// per agent override
	os.MkdirAll(filepath.Join(tmp, "agents", "researcher"), 0755)
	os.WriteFile(filepath.Join(tmp, "agents", "researcher", "SETTINGS.yaml"), []byte("model: qwen-special\nmax_tokens: 4096\n"), 0644)

	ctx, err := LoadForAgent(tmp, "researcher")
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := ctx.SETTINGS["model"].(string); !ok || m != "qwen-special" {
		t.Errorf("expected researcher override model, got %+v", ctx.SETTINGS)
	}

	// Validate
	if err := ValidateSettings(ctx.SETTINGS); err != nil {
		t.Errorf("valid settings rejected: %v", err)
	}

	// bad autonomy
	bad := map[string]interface{}{"autonomy_level": 99}
	if err := ValidateSettings(bad); err == nil {
		t.Error("expected validate error for bad autonomy")
	}
}

func TestWriteSettingsAtomic(t *testing.T) {
	tmp := t.TempDir()
	s := map[string]interface{}{"model": "test-m", "temperature": 0.2}
	if err := WriteSettingsAtomic(tmp, "tester", s); err != nil {
		t.Fatal(err)
	}
	// file created
	b, _ := os.ReadFile(filepath.Join(tmp, "agents", "tester", "SETTINGS.yaml"))
	if !strings.Contains(string(b), "test-m") {
		t.Error("settings not written")
	}
}
