package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.IsEmpty() {
		t.Fatal("expected empty content for empty directory")
	}
}

func TestLoad_MissingDir(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("unexpected error for missing dir: %v", err)
	}
	if !c.IsEmpty() {
		t.Fatal("expected empty content for missing directory")
	}
}

func TestLoad_EmptyPath(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error for empty path: %v", err)
	}
	if !c.IsEmpty() {
		t.Fatal("expected empty content for empty path")
	}
}

func TestLoad_AllFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"AGENTS.md": "You are a pirate.",
		"SOUL.md":   "Be kind.",
		"TOOLS.md":  "Prefer script.run.",
		"SKILL.md":  "Context for skill builds.",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	c, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.IsEmpty() {
		t.Fatal("expected non-empty content")
	}
	if c.Agents != "You are a pirate." {
		t.Errorf("AGENTS.md: got %q", c.Agents)
	}
	if c.Soul != "Be kind." {
		t.Errorf("SOUL.md: got %q", c.Soul)
	}
	if c.Tools != "Prefer script.run." {
		t.Errorf("TOOLS.md: got %q", c.Tools)
	}
	if c.Skill != "Context for skill builds." {
		t.Errorf("SKILL.md: got %q", c.Skill)
	}
}

func TestLoad_PartialFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("  Be humble.  "), 0600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Agents != "" || c.Tools != "" || c.Skill != "" {
		t.Error("expected empty fields for absent files")
	}
	if c.Soul != "Be humble." {
		t.Errorf("SOUL.md: got %q", c.Soul)
	}
}

func TestLoad_FileTooLarge(t *testing.T) {
	dir := t.TempDir()
	large := strings.Repeat("x", maxFileBytes+1)
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(large), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
}

func TestLoad_NotADirectory(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "notadir")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	_, err = Load(f.Name())
	if err == nil {
		t.Fatal("expected error when path is a file not a directory")
	}
}
