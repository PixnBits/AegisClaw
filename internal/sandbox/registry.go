package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// SkillState represents the lifecycle state of a registered skill.
type SkillState string

const (
	SkillStateInactive SkillState = "inactive"
	SkillStateActive   SkillState = "active"
	SkillStateStopped  SkillState = "stopped"
	SkillStateError    SkillState = "error"
)

// SkillEntry is a single skill in the persistent registry.
type SkillEntry struct {
	Name        string            `json:"name"`
	SandboxID   string            `json:"sandbox_id"`
	State       SkillState        `json:"state"`
	ActivatedAt *time.Time        `json:"activated_at,omitempty"`
	StoppedAt   *time.Time        `json:"stopped_at,omitempty"`
	MerkleHash  string            `json:"merkle_hash"`
	PrevHash    string            `json:"prev_hash"`
	Version     int               `json:"version"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// RegistrySnapshot is the top-level persisted registry structure.
type RegistrySnapshot struct {
	Skills    map[string]*SkillEntry `json:"skills"`
	RootHash  string                 `json:"root_hash"`
	UpdatedAt time.Time              `json:"updated_at"`
	Sequence  uint64                 `json:"sequence"`
}

// SkillRegistry manages the persistent, tamper-evident skill registry.
type SkillRegistry struct {
	path     string
	snapshot RegistrySnapshot
	mu       sync.RWMutex
}

// NewSkillRegistry loads or creates a skill registry at the given path.
func NewSkillRegistry(path string) (*SkillRegistry, error) {
	if path == "" {
		return nil, fmt.Errorf("registry path is required")
	}

	r := &SkillRegistry{
		path: path,
		snapshot: RegistrySnapshot{
			Skills: make(map[string]*SkillEntry),
		},
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read registry %s: %w", path, err)
	}

	if err := json.Unmarshal(data, &r.snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal registry: %w", err)
	}
	if r.snapshot.Skills == nil {
		r.snapshot.Skills = make(map[string]*SkillEntry)
	}

	if err := r.verifyIntegrity(); err != nil {
		// Attempt to repair the registry by recalculating the root hash.
		// This handles cases where the hash is stale (e.g., after a crash).
		if repairErr := r.repairIntegrity(); repairErr != nil {
			return nil, fmt.Errorf("registry integrity check failed: %w, repair also failed: %w", err, repairErr)
		}
	}

	return r, nil
}

// Register adds or updates a skill entry in the registry.
func (r *SkillRegistry) Register(name, sandboxID string, metadata map[string]string) (*SkillEntry, error) {
	if name == "" {
		return nil, fmt.Errorf("skill name is required")
	}
	if sandboxID == "" {
		return nil, fmt.Errorf("sandbox ID is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()
	prevHash := ""
	version := 1

	if existing, ok := r.snapshot.Skills[name]; ok {
		prevHash = existing.MerkleHash
		version = existing.Version + 1
	}

	entry := &SkillEntry{
		Name:        name,
		SandboxID:   sandboxID,
		State:       SkillStateActive,
		ActivatedAt: &now,
		PrevHash:    prevHash,
		Version:     version,
		Metadata:    metadata,
	}

	entry.MerkleHash = computeEntryHash(entry)
	r.snapshot.Skills[name] = entry

	if err := r.persistLocked(); err != nil {
		return nil, fmt.Errorf("failed to persist registry: %w", err)
	}

	return entry, nil
}

// Deactivate marks a skill as stopped.
func (r *SkillRegistry) Deactivate(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.snapshot.Skills[name]
	if !ok {
		return fmt.Errorf("skill %q not found in registry", name)
	}
	if entry.State != SkillStateActive {
		return fmt.Errorf("skill %q is not active (state: %s)", name, entry.State)
	}

	now := time.Now().UTC()
	entry.PrevHash = entry.MerkleHash
	entry.State = SkillStateStopped
	entry.StoppedAt = &now
	entry.Version++
	entry.MerkleHash = computeEntryHash(entry)

	return r.persistLocked()
}

// SetError marks a skill as errored.
func (r *SkillRegistry) SetError(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.snapshot.Skills[name]
	if !ok {
		return fmt.Errorf("skill %q not found in registry", name)
	}

	entry.PrevHash = entry.MerkleHash
	entry.State = SkillStateError
	entry.Version++
	entry.MerkleHash = computeEntryHash(entry)

	return r.persistLocked()
}

// Get returns a skill entry by name.
func (r *SkillRegistry) Get(name string) (*SkillEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.snapshot.Skills[name]
	if !ok {
		return nil, false
	}
	copy := *e
	return &copy, true
}

// List returns all skill entries.
func (r *SkillRegistry) List() []SkillEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]SkillEntry, 0, len(r.snapshot.Skills))
	for _, e := range r.snapshot.Skills {
		result = append(result, *e)
	}
	return result
}

// RootHash returns the current Merkle root hash.
func (r *SkillRegistry) RootHash() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot.RootHash
}

// Sequence returns the current mutation sequence number.
func (r *SkillRegistry) Sequence() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot.Sequence
}

func (r *SkillRegistry) verifyIntegrity() error {
	expected := computeRootHash(r.snapshot.Skills)
	if r.snapshot.RootHash != "" && r.snapshot.RootHash != expected {
		return fmt.Errorf("root hash mismatch: stored=%s computed=%s", r.snapshot.RootHash, expected)
	}
	return nil
}

// repairIntegrity recalculates and re-persists the root hash if it's mismatched.
// This is used for recovery when the registry file becomes corrupted or stale.
func (r *SkillRegistry) repairIntegrity() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Recalculate the expected root hash based on current skills.
	expected := computeRootHash(r.snapshot.Skills)
	if r.snapshot.RootHash == expected {
		return nil // Hash is already correct.
	}

	// Update and re-persist.
	return r.persistLocked()
}

func (r *SkillRegistry) persistLocked() error {
	r.snapshot.Sequence++
	r.snapshot.UpdatedAt = time.Now().UTC()
	r.snapshot.RootHash = computeRootHash(r.snapshot.Skills)

	data, err := json.MarshalIndent(r.snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registry: %w", err)
	}

	tmpPath := r.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write registry: %w", err)
	}

	if err := os.Rename(tmpPath, r.path); err != nil {
		return fmt.Errorf("failed to rename registry: %w", err)
	}

	return nil
}

func computeEntryHash(e *SkillEntry) string {
	h := sha256.New()
	h.Write([]byte(e.Name))
	h.Write([]byte(e.SandboxID))
	h.Write([]byte(e.State))
	h.Write([]byte(e.PrevHash))
	h.Write([]byte(fmt.Sprintf("%d", e.Version)))
	if e.ActivatedAt != nil {
		h.Write([]byte(e.ActivatedAt.Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func computeRootHash(skills map[string]*SkillEntry) string {
	h := sha256.New()
	for _, entry := range skills {
		h.Write([]byte(entry.MerkleHash))
	}
	return hex.EncodeToString(h.Sum(nil))
}
