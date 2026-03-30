// Package composition implements versioned composition manifests for
// tracking and rolling back deployed component versions.
//
// Per the PRD (§13.2), updates follow a versioned composition model:
//   - Code generation produces a new revision of the affected microVM component(s).
//   - The composition manifest is edited to reference the new version(s) and
//     stored as a new composition version.
//   - Each microVM includes built-in healthchecks. The in-VM controller signals
//     degradation or failure, triggering automatic rollback to the previous
//     composition version (roll-forward safety).
//
// This package resolves deviation D10 from the PRD alignment plan.
package composition

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ComponentType classifies the kind of managed component.
type ComponentType string

const (
	ComponentSkill       ComponentType = "skill"
	ComponentReviewer    ComponentType = "reviewer"
	ComponentBuilder     ComponentType = "builder"
	ComponentMainAgent   ComponentType = "main-agent"
	ComponentCoordinator ComponentType = "coordinator"
	// ComponentHub is the AegisHub system microVM — the sole IPC router.
	// It is always the first component launched and the last stopped.
	// Changes to AegisHub must flow through the Governance Court SDLC.
	ComponentHub ComponentType = "hub"
)

// HealthStatus represents a component's health.
type HealthStatus string

const (
	HealthHealthy   HealthStatus = "healthy"
	HealthDegraded  HealthStatus = "degraded"
	HealthUnhealthy HealthStatus = "unhealthy"
	HealthUnknown   HealthStatus = "unknown"
)

// Component describes a single managed component in the composition.
type Component struct {
	Name        string            `json:"name"`
	Type        ComponentType     `json:"type"`
	Version     string            `json:"version"`
	SandboxID   string            `json:"sandbox_id,omitempty"`
	ArtifactRef string            `json:"artifact_ref,omitempty"`
	RootfsHash  string            `json:"rootfs_hash,omitempty"`
	Health      HealthStatus      `json:"health"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	LastChecked time.Time         `json:"last_checked"`
}

// Manifest represents a versioned composition of all active components.
// It is the source of truth for what should be running.
type Manifest struct {
	Version    int                  `json:"version"`
	Components map[string]Component `json:"components"`
	Hash       string               `json:"hash"`
	CreatedAt  time.Time            `json:"created_at"`
	CreatedBy  string               `json:"created_by"`
	Reason     string               `json:"reason,omitempty"`
}

// ComputeHash computes a deterministic SHA-256 hash of the manifest's components.
func (m *Manifest) ComputeHash() string {
	data, _ := json.Marshal(m.Components)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Validate checks that the manifest has required fields.
func (m *Manifest) Validate() error {
	if m.Version < 1 {
		return fmt.Errorf("manifest version must be positive, got %d", m.Version)
	}
	if len(m.Components) == 0 {
		return fmt.Errorf("manifest must have at least one component")
	}
	for name, c := range m.Components {
		if c.Name != name {
			return fmt.Errorf("component name mismatch: key %q vs name %q", name, c.Name)
		}
		if c.Version == "" {
			return fmt.Errorf("component %q has empty version", name)
		}
	}
	return nil
}

// HealthSummary returns counts of healthy, degraded, and unhealthy components.
func (m *Manifest) HealthSummary() (healthy, degraded, unhealthy int) {
	for _, c := range m.Components {
		switch c.Health {
		case HealthHealthy:
			healthy++
		case HealthDegraded:
			degraded++
		case HealthUnhealthy:
			unhealthy++
		default:
			unhealthy++ // Unknown treated as unhealthy for safety.
		}
	}
	return
}

// Store manages versioned composition manifests on disk.
// Each version is stored as a separate JSON file under the store directory.
type Store struct {
	dir     string
	mu      sync.RWMutex
	current *Manifest
}

// NewStore creates or opens a composition store at the given directory.
func NewStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("composition store directory is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create composition store: %w", err)
	}

	s := &Store{dir: dir}

	// Load the latest manifest if it exists.
	latest, err := s.loadLatest()
	if err != nil {
		return nil, fmt.Errorf("failed to load latest manifest: %w", err)
	}
	s.current = latest

	return s, nil
}

// Current returns the active composition manifest (or nil if none exists).
func (s *Store) Current() *Manifest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// CurrentVersion returns the current manifest version (0 if no manifest).
func (s *Store) CurrentVersion() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current == nil {
		return 0
	}
	return s.current.Version
}

// Publish creates a new composition version from the given components.
func (s *Store) Publish(components map[string]Component, actor, reason string) (*Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	version := 1
	if s.current != nil {
		version = s.current.Version + 1
	}

	manifest := &Manifest{
		Version:    version,
		Components: components,
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  actor,
		Reason:     reason,
	}
	manifest.Hash = manifest.ComputeHash()

	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}

	if err := s.save(manifest); err != nil {
		return nil, fmt.Errorf("failed to save manifest: %w", err)
	}

	s.current = manifest
	return manifest, nil
}

// UpdateHealth updates the health status of a component in the current manifest.
func (s *Store) UpdateHealth(name string, health HealthStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.current == nil {
		return fmt.Errorf("no active composition")
	}

	c, ok := s.current.Components[name]
	if !ok {
		return fmt.Errorf("component %q not found in current composition", name)
	}

	c.Health = health
	c.LastChecked = time.Now().UTC()
	s.current.Components[name] = c

	return s.save(s.current)
}

// Get loads a specific manifest version.
func (s *Store) Get(version int) (*Manifest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dir, fmt.Sprintf("v%d.json", version))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("manifest v%d not found: %w", version, err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid manifest v%d: %w", version, err)
	}
	return &m, nil
}

// Rollback restores a previous manifest version as the new current.
// It creates a new version entry that references the rolled-back components.
func (s *Store) Rollback(targetVersion int, actor, reason string) (*Manifest, error) {
	target, err := s.Get(targetVersion)
	if err != nil {
		return nil, fmt.Errorf("cannot rollback to v%d: %w", targetVersion, err)
	}

	rollbackReason := fmt.Sprintf("rollback to v%d: %s", targetVersion, reason)
	return s.Publish(target.Components, actor, rollbackReason)
}

// RollbackToPrevious rolls back to version N-1 (current version minus one).
func (s *Store) RollbackToPrevious(actor, reason string) (*Manifest, error) {
	s.mu.RLock()
	currentVer := 0
	if s.current != nil {
		currentVer = s.current.Version
	}
	s.mu.RUnlock()

	if currentVer < 2 {
		return nil, fmt.Errorf("no previous version to rollback to (current: v%d)", currentVer)
	}

	return s.Rollback(currentVer-1, actor, reason)
}

// History returns all manifest versions in chronological order.
func (s *Store) History() ([]*Manifest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read composition store: %w", err)
	}

	var manifests []*Manifest
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var m Manifest
		if json.Unmarshal(data, &m) == nil {
			manifests = append(manifests, &m)
		}
	}

	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].Version < manifests[j].Version
	})

	return manifests, nil
}

// NeedsRollback returns true if any component is unhealthy.
func (s *Store) NeedsRollback() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.current == nil {
		return false
	}

	_, _, unhealthy := s.current.HealthSummary()
	return unhealthy > 0
}

// save writes a manifest to disk.
func (s *Store) save(m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(s.dir, fmt.Sprintf("v%d.json", m.Version))
	return os.WriteFile(path, data, 0600)
}

// loadLatest reads the highest-versioned manifest from disk.
func (s *Store) loadLatest() (*Manifest, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var latest *Manifest
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var m Manifest
		if json.Unmarshal(data, &m) == nil {
			if latest == nil || m.Version > latest.Version {
				latest = &m
			}
		}
	}

	return latest, nil
}
