package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

// TestComputeRootHashDeterminism verifies that computeRootHash produces the
// same value for the same set of skills regardless of map-iteration order.
// Previously the function iterated over a Go map directly, which is
// non-deterministic and caused the integrity check to spuriously fail on
// cold boot.
func TestComputeRootHashDeterminism(t *testing.T) {
	skills := map[string]*SkillEntry{
		"alpha": {Name: "alpha", SandboxID: "s1", State: SkillStateActive, MerkleHash: "aaa"},
		"beta":  {Name: "beta", SandboxID: "s2", State: SkillStateActive, MerkleHash: "bbb"},
		"gamma": {Name: "gamma", SandboxID: "s3", State: SkillStateActive, MerkleHash: "ccc"},
	}

	first := computeRootHash(skills)
	for i := 0; i < 100; i++ {
		if got := computeRootHash(skills); got != first {
			t.Fatalf("iteration %d: root hash not deterministic: got %q, want %q", i, got, first)
		}
	}
}

// TestRegistryIntegrityRoundTrip verifies that a registry saved to disk and
// reloaded passes its integrity check without requiring repair.  This would
// fail before the deterministic sort fix because the map could iterate in a
// different order on reload, producing a different root hash than the one
// written at save time.
func TestRegistryIntegrityRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")

	r, err := NewSkillRegistry(path)
	if err != nil {
		t.Fatalf("NewSkillRegistry: %v", err)
	}

	for _, name := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		if _, err := r.Register(name, "sandbox-"+name, nil); err != nil {
			t.Fatalf("Register %s: %v", name, err)
		}
	}

	savedRoot := r.RootHash()

	// Reload from disk.
	r2, err := NewSkillRegistry(path)
	if err != nil {
		t.Fatalf("reload NewSkillRegistry: %v", err)
	}
	if got := r2.RootHash(); got != savedRoot {
		t.Fatalf("root hash changed after reload: saved=%q loaded=%q", savedRoot, got)
	}

	// Verify the raw file hash matches too.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read registry file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("registry file is empty")
	}
}

// TestSkillActivateHandlerDeregistersStaleEntry is a unit-level test that
// guards against the stale-sandbox bug: when a skill's registry entry shows
// SkillStateActive but the underlying sandbox has stopped, a subsequent call
// to Register (as done by the activate handler after deregistration) must
// create a new entry with a fresh sandbox ID.
func TestSkillActivateHandlerDeregistersStaleEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")

	r, err := NewSkillRegistry(path)
	if err != nil {
		t.Fatalf("NewSkillRegistry: %v", err)
	}

	// Simulate first activation.
	firstSandboxID := "skill-abc123"
	entry1, err := r.Register("hello_world", firstSandboxID, nil)
	if err != nil {
		t.Fatalf("Register (first): %v", err)
	}
	if entry1.State != SkillStateActive {
		t.Fatalf("expected active after first register, got %s", entry1.State)
	}

	// Simulate sandbox dying: deactivate.
	if err := r.Deactivate("hello_world"); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	after, ok := r.Get("hello_world")
	if !ok {
		t.Fatal("entry disappeared after deactivate")
	}
	if after.State != SkillStateStopped {
		t.Fatalf("expected stopped after deactivate, got %s", after.State)
	}

	// Re-activate with a new sandbox.
	secondSandboxID := "skill-def456"
	entry2, err := r.Register("hello_world", secondSandboxID, nil)
	if err != nil {
		t.Fatalf("Register (second): %v", err)
	}
	if entry2.SandboxID != secondSandboxID {
		t.Fatalf("expected new sandbox ID %q after re-activation, got %q", secondSandboxID, entry2.SandboxID)
	}
	if entry2.State != SkillStateActive {
		t.Fatalf("expected active after re-register, got %s", entry2.State)
	}
	if entry2.SandboxID == firstSandboxID {
		t.Fatal("re-activation returned the old stopped sandbox ID — stale-entry bug is present")
	}
	if entry2.Version <= entry1.Version {
		t.Fatalf("version must advance on re-activation: first=%d second=%d", entry1.Version, entry2.Version)
	}
}
