package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// ModelEntry represents a known-good model in the registry with its verification hash and tags.
type ModelEntry struct {
	Name     string   `json:"name"`
	SHA256   string   `json:"sha256"`
	Tags     []string `json:"tags"`
	SizeHint int64    `json:"size_hint,omitempty"`
}

// HasTag returns true if the model has the given tag.
func (e ModelEntry) HasTag(tag string) bool {
	for _, t := range e.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// ModelRegistry maintains a set of known-good models with SHA256 hashes and persona-suitability tags.
type ModelRegistry struct {
	mu      sync.RWMutex
	path    string
	entries map[string]ModelEntry
}

// NewModelRegistry creates a registry from a JSON file. If the file does not exist, an empty registry is created.
func NewModelRegistry(path string) (*ModelRegistry, error) {
	r := &ModelRegistry{
		path:    path,
		entries: make(map[string]ModelEntry),
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read model registry %s: %w", path, err)
	}

	var entries []ModelEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse model registry: %w", err)
	}
	for _, e := range entries {
		r.entries[e.Name] = e
	}
	return r, nil
}

// Get returns a model entry by name.
func (r *ModelRegistry) Get(name string) (ModelEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[name]
	return e, ok
}

// List returns all registered models.
func (r *ModelRegistry) List() []ModelEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]ModelEntry, 0, len(r.entries))
	for _, e := range r.entries {
		result = append(result, e)
	}
	return result
}

// ByTag returns all models that carry the given tag.
func (r *ModelRegistry) ByTag(tag string) []ModelEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []ModelEntry
	for _, e := range r.entries {
		if e.HasTag(tag) {
			result = append(result, e)
		}
	}
	return result
}

// Register adds or updates a model entry and persists the registry.
func (r *ModelRegistry) Register(entry ModelEntry) error {
	if entry.Name == "" {
		return fmt.Errorf("model name is required")
	}
	if entry.SHA256 == "" {
		return fmt.Errorf("model SHA256 hash is required")
	}
	r.mu.Lock()
	r.entries[entry.Name] = entry
	r.mu.Unlock()
	return r.save()
}

// registerSeed adds a model entry without requiring a SHA256 hash (for known-good seed entries).
func (r *ModelRegistry) registerSeed(entry ModelEntry) error {
	if entry.Name == "" {
		return fmt.Errorf("model name is required")
	}
	r.mu.Lock()
	r.entries[entry.Name] = entry
	r.mu.Unlock()
	return r.save()
}

// Remove deletes a model from the registry and persists.
func (r *ModelRegistry) Remove(name string) error {
	r.mu.Lock()
	delete(r.entries, name)
	r.mu.Unlock()
	return r.save()
}

// Count returns the number of registered models.
func (r *ModelRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

func (r *ModelRegistry) save() error {
	r.mu.RLock()
	entries := make([]ModelEntry, 0, len(r.entries))
	for _, e := range r.entries {
		entries = append(entries, e)
	}
	r.mu.RUnlock()

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal model registry: %w", err)
	}
	return os.WriteFile(r.path, data, 0600)
}
