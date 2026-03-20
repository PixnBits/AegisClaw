package llm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewModelRegistry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")

	r, err := NewModelRegistry(path)
	if err != nil {
		t.Fatalf("NewModelRegistry failed: %v", err)
	}
	if r.Count() != 0 {
		t.Errorf("expected empty registry, got %d", r.Count())
	}
}

func TestModelRegistryRegisterAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	r, _ := NewModelRegistry(path)

	err := r.Register(ModelEntry{
		Name:   "mistral-nemo",
		SHA256: "abc123",
		Tags:   []string{"security", "code_review"},
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	entry, ok := r.Get("mistral-nemo")
	if !ok {
		t.Fatal("expected to find mistral-nemo")
	}
	if entry.SHA256 != "abc123" {
		t.Errorf("unexpected sha256: %s", entry.SHA256)
	}
	if !entry.HasTag("security") {
		t.Error("expected security tag")
	}
}

func TestModelRegistryPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")

	r1, _ := NewModelRegistry(path)
	r1.Register(ModelEntry{Name: "test-model", SHA256: "hash1", Tags: []string{"test"}})

	// Reload from disk
	r2, err := NewModelRegistry(path)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if r2.Count() != 1 {
		t.Errorf("expected 1 model, got %d", r2.Count())
	}
	entry, ok := r2.Get("test-model")
	if !ok {
		t.Fatal("expected to find test-model after reload")
	}
	if entry.SHA256 != "hash1" {
		t.Errorf("unexpected sha256 after reload: %s", entry.SHA256)
	}
}

func TestModelRegistryList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	r, _ := NewModelRegistry(path)

	r.Register(ModelEntry{Name: "a", SHA256: "h1", Tags: []string{"x"}})
	r.Register(ModelEntry{Name: "b", SHA256: "h2", Tags: []string{"y"}})

	list := r.List()
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}
}

func TestModelRegistryByTag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	r, _ := NewModelRegistry(path)

	r.Register(ModelEntry{Name: "a", SHA256: "h1", Tags: []string{"security", "code"}})
	r.Register(ModelEntry{Name: "b", SHA256: "h2", Tags: []string{"code"}})
	r.Register(ModelEntry{Name: "c", SHA256: "h3", Tags: []string{"test"}})

	security := r.ByTag("security")
	if len(security) != 1 {
		t.Errorf("expected 1 security model, got %d", len(security))
	}
	if security[0].Name != "a" {
		t.Errorf("expected 'a', got %q", security[0].Name)
	}

	code := r.ByTag("code")
	if len(code) != 2 {
		t.Errorf("expected 2 code models, got %d", len(code))
	}
}

func TestModelRegistryRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	r, _ := NewModelRegistry(path)

	r.Register(ModelEntry{Name: "x", SHA256: "h1", Tags: nil})
	r.Remove("x")

	if r.Count() != 0 {
		t.Errorf("expected 0 after remove, got %d", r.Count())
	}
	_, ok := r.Get("x")
	if ok {
		t.Error("should not find removed entry")
	}
}

func TestModelRegistryRegisterValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	r, _ := NewModelRegistry(path)

	if err := r.Register(ModelEntry{Name: "", SHA256: "abc"}); err == nil {
		t.Error("expected error for empty name")
	}
	if err := r.Register(ModelEntry{Name: "x", SHA256: ""}); err == nil {
		t.Error("expected error for empty SHA256")
	}
}

func TestModelRegistryCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	os.WriteFile(path, []byte("not json"), 0600)

	_, err := NewModelRegistry(path)
	if err == nil {
		t.Error("expected error for corrupt file")
	}
}

func TestModelEntryHasTag(t *testing.T) {
	e := ModelEntry{Tags: []string{"a", "b", "c"}}
	if !e.HasTag("b") {
		t.Error("expected HasTag(b)=true")
	}
	if e.HasTag("d") {
		t.Error("expected HasTag(d)=false")
	}
}

func TestModelRegistryUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	r, _ := NewModelRegistry(path)

	r.Register(ModelEntry{Name: "m", SHA256: "old", Tags: []string{"x"}})
	r.Register(ModelEntry{Name: "m", SHA256: "new", Tags: []string{"y"}})

	entry, _ := r.Get("m")
	if entry.SHA256 != "new" {
		t.Errorf("expected updated sha256 'new', got %q", entry.SHA256)
	}
	if r.Count() != 1 {
		t.Errorf("expected 1 entry after update, got %d", r.Count())
	}
}
