package composition

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	if store.Current() != nil {
		t.Error("expected nil current manifest for new store")
	}
	if store.CurrentVersion() != 0 {
		t.Errorf("expected version 0, got %d", store.CurrentVersion())
	}
}

func TestNewStoreEmptyDir(t *testing.T) {
	_, err := NewStore("")
	if err == nil {
		t.Error("expected error for empty directory")
	}
}

func TestPublishAndCurrent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	components := map[string]Component{
		"hello-skill": {
			Name:    "hello-skill",
			Type:    ComponentSkill,
			Version: "1.0.0",
			Health:  HealthHealthy,
		},
	}

	m, err := store.Publish(components, "operator", "initial deployment")
	if err != nil {
		t.Fatalf("Publish() error: %v", err)
	}

	if m.Version != 1 {
		t.Errorf("expected version 1, got %d", m.Version)
	}
	if m.CreatedBy != "operator" {
		t.Errorf("expected creator 'operator', got %q", m.CreatedBy)
	}
	if m.Hash == "" {
		t.Error("expected non-empty hash")
	}
	if m.Reason != "initial deployment" {
		t.Errorf("expected reason 'initial deployment', got %q", m.Reason)
	}

	current := store.Current()
	if current == nil {
		t.Fatal("expected non-nil current manifest")
	}
	if current.Version != 1 {
		t.Errorf("expected current version 1, got %d", current.Version)
	}

	// Publish a second version.
	components["hello-skill"] = Component{
		Name:    "hello-skill",
		Type:    ComponentSkill,
		Version: "1.1.0",
		Health:  HealthHealthy,
	}
	m2, err := store.Publish(components, "daemon", "skill update")
	if err != nil {
		t.Fatalf("Publish() v2 error: %v", err)
	}
	if m2.Version != 2 {
		t.Errorf("expected version 2, got %d", m2.Version)
	}
}

func TestPublishValidation(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	// Empty components should fail.
	_, err = store.Publish(map[string]Component{}, "operator", "bad")
	if err == nil {
		t.Error("expected error for empty components")
	}

	// Missing version should fail.
	_, err = store.Publish(map[string]Component{
		"test": {Name: "test", Type: ComponentSkill, Version: ""},
	}, "operator", "bad")
	if err == nil {
		t.Error("expected error for empty version")
	}
}

func TestGet(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	components := map[string]Component{
		"test": {Name: "test", Type: ComponentSkill, Version: "1.0.0", Health: HealthHealthy},
	}
	_, err = store.Publish(components, "operator", "v1")
	if err != nil {
		t.Fatalf("Publish() error: %v", err)
	}

	m, err := store.Get(1)
	if err != nil {
		t.Fatalf("Get(1) error: %v", err)
	}
	if m.Version != 1 {
		t.Errorf("expected version 1, got %d", m.Version)
	}

	// Non-existent version.
	_, err = store.Get(999)
	if err == nil {
		t.Error("expected error for non-existent version")
	}
}

func TestRollback(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	// Publish v1.
	v1Components := map[string]Component{
		"test": {Name: "test", Type: ComponentSkill, Version: "1.0.0", Health: HealthHealthy},
	}
	_, err = store.Publish(v1Components, "operator", "v1 deploy")
	if err != nil {
		t.Fatalf("Publish v1 error: %v", err)
	}

	// Publish v2.
	v2Components := map[string]Component{
		"test": {Name: "test", Type: ComponentSkill, Version: "2.0.0", Health: HealthUnhealthy},
	}
	_, err = store.Publish(v2Components, "operator", "v2 deploy")
	if err != nil {
		t.Fatalf("Publish v2 error: %v", err)
	}

	if store.CurrentVersion() != 2 {
		t.Fatalf("expected current version 2, got %d", store.CurrentVersion())
	}

	// Rollback to v1.
	m, err := store.Rollback(1, "operator", "v2 unhealthy")
	if err != nil {
		t.Fatalf("Rollback error: %v", err)
	}

	// Rollback creates a new version (v3) with v1's components.
	if m.Version != 3 {
		t.Errorf("expected rollback version 3, got %d", m.Version)
	}

	comp, ok := m.Components["test"]
	if !ok {
		t.Fatal("expected 'test' component in rolled-back manifest")
	}
	if comp.Version != "1.0.0" {
		t.Errorf("expected component version '1.0.0', got %q", comp.Version)
	}
}

func TestRollbackToPrevious(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	// Only v1 — no previous to rollback to.
	_, err = store.Publish(map[string]Component{
		"test": {Name: "test", Type: ComponentSkill, Version: "1.0.0", Health: HealthHealthy},
	}, "operator", "initial")
	if err != nil {
		t.Fatalf("Publish v1 error: %v", err)
	}

	_, err = store.RollbackToPrevious("operator", "nope")
	if err == nil {
		t.Error("expected error when rolling back from v1")
	}

	// Publish v2.
	_, err = store.Publish(map[string]Component{
		"test": {Name: "test", Type: ComponentSkill, Version: "2.0.0", Health: HealthHealthy},
	}, "operator", "v2")
	if err != nil {
		t.Fatalf("Publish v2 error: %v", err)
	}

	m, err := store.RollbackToPrevious("operator", "v2 failed")
	if err != nil {
		t.Fatalf("RollbackToPrevious error: %v", err)
	}
	if m.Version != 3 {
		t.Errorf("expected version 3, got %d", m.Version)
	}
}

func TestUpdateHealth(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	_, err = store.Publish(map[string]Component{
		"test": {Name: "test", Type: ComponentSkill, Version: "1.0.0", Health: HealthHealthy},
	}, "operator", "initial")
	if err != nil {
		t.Fatalf("Publish error: %v", err)
	}

	if err := store.UpdateHealth("test", HealthUnhealthy); err != nil {
		t.Fatalf("UpdateHealth error: %v", err)
	}

	current := store.Current()
	if current.Components["test"].Health != HealthUnhealthy {
		t.Errorf("expected unhealthy, got %s", current.Components["test"].Health)
	}

	// Unknown component.
	if err := store.UpdateHealth("nonexistent", HealthHealthy); err == nil {
		t.Error("expected error for unknown component")
	}
}

func TestNeedsRollback(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	// No manifest → no rollback needed.
	if store.NeedsRollback() {
		t.Error("expected no rollback needed for empty store")
	}

	// All healthy.
	_, err = store.Publish(map[string]Component{
		"test": {Name: "test", Type: ComponentSkill, Version: "1.0.0", Health: HealthHealthy},
	}, "operator", "initial")
	if err != nil {
		t.Fatalf("Publish error: %v", err)
	}
	if store.NeedsRollback() {
		t.Error("expected no rollback for healthy composition")
	}

	// Mark unhealthy.
	store.UpdateHealth("test", HealthUnhealthy)
	if !store.NeedsRollback() {
		t.Error("expected rollback needed for unhealthy composition")
	}
}

func TestHealthSummary(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Components: map[string]Component{
			"a": {Name: "a", Type: ComponentSkill, Version: "1.0", Health: HealthHealthy},
			"b": {Name: "b", Type: ComponentSkill, Version: "1.0", Health: HealthDegraded},
			"c": {Name: "c", Type: ComponentSkill, Version: "1.0", Health: HealthUnhealthy},
			"d": {Name: "d", Type: ComponentSkill, Version: "1.0", Health: HealthUnknown},
		},
	}

	h, d, u := m.HealthSummary()
	if h != 1 {
		t.Errorf("expected 1 healthy, got %d", h)
	}
	if d != 1 {
		t.Errorf("expected 1 degraded, got %d", d)
	}
	if u != 2 { // unhealthy + unknown
		t.Errorf("expected 2 unhealthy, got %d", u)
	}
}

func TestHistory(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	for i := 0; i < 3; i++ {
		_, err := store.Publish(map[string]Component{
			"test": {Name: "test", Type: ComponentSkill, Version: "1.0.0", Health: HealthHealthy},
		}, "operator", "deploy")
		if err != nil {
			t.Fatalf("Publish error: %v", err)
		}
	}

	history, err := store.History()
	if err != nil {
		t.Fatalf("History() error: %v", err)
	}
	if len(history) != 3 {
		t.Errorf("expected 3 entries, got %d", len(history))
	}
	// Verify order.
	for i := 0; i < len(history)-1; i++ {
		if history[i].Version >= history[i+1].Version {
			t.Errorf("history not in order: v%d >= v%d", history[i].Version, history[i+1].Version)
		}
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	_, err = store.Publish(map[string]Component{
		"test": {Name: "test", Type: ComponentSkill, Version: "1.0.0", Health: HealthHealthy},
	}, "operator", "initial")
	if err != nil {
		t.Fatalf("Publish error: %v", err)
	}

	// Verify file was written.
	path := filepath.Join(dir, "v1.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("manifest file not found: %v", err)
	}

	// Create a new store instance from the same directory.
	store2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() reopen error: %v", err)
	}
	if store2.CurrentVersion() != 1 {
		t.Errorf("expected reloaded version 1, got %d", store2.CurrentVersion())
	}
}

func TestManifestValidate(t *testing.T) {
	tests := []struct {
		name    string
		m       Manifest
		wantErr bool
	}{
		{
			name: "valid",
			m: Manifest{
				Version:    1,
				Components: map[string]Component{"a": {Name: "a", Version: "1.0"}},
			},
		},
		{
			name:    "zero version",
			m:       Manifest{Version: 0, Components: map[string]Component{"a": {Name: "a", Version: "1.0"}}},
			wantErr: true,
		},
		{
			name:    "empty components",
			m:       Manifest{Version: 1, Components: map[string]Component{}},
			wantErr: true,
		},
		{
			name: "name mismatch",
			m: Manifest{
				Version:    1,
				Components: map[string]Component{"a": {Name: "b", Version: "1.0"}},
			},
			wantErr: true,
		},
		{
			name: "empty component version",
			m: Manifest{
				Version:    1,
				Components: map[string]Component{"a": {Name: "a", Version: ""}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.m.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestComponentHubType verifies that ComponentHub is a distinct type and can
// be stored and retrieved from the composition store. AegisHub must always be
// the first component in the manifest on daemon startup.
func TestComponentHubType(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	if ComponentHub == ComponentSkill || ComponentHub == ComponentMainAgent {
		t.Error("ComponentHub must be a distinct type from other component types")
	}

	components := map[string]Component{
		"aegishub": {
			Name:        "aegishub",
			Type:        ComponentHub,
			Version:     "1",
			SandboxID:   "aegishub-abc12345",
			ArtifactRef: "/var/lib/aegisclaw/rootfs-templates/aegishub-rootfs.ext4",
			Health:      HealthHealthy,
		},
	}

	m, err := store.Publish(components, "daemon", "AegisHub microVM launched")
	if err != nil {
		t.Fatalf("Publish() error: %v", err)
	}

	hub, ok := m.Components["aegishub"]
	if !ok {
		t.Fatal("expected aegishub component in manifest")
	}
	if hub.Type != ComponentHub {
		t.Errorf("expected ComponentHub type, got %q", hub.Type)
	}
	if hub.SandboxID != "aegishub-abc12345" {
		t.Errorf("expected sandbox ID to be preserved, got %q", hub.SandboxID)
	}

	// Verify the composition store correctly persists and reloads the hub entry.
	store2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() reload error: %v", err)
	}
	current := store2.Current()
	if current == nil {
		t.Fatal("expected non-nil current manifest after reload")
	}
	if _, ok := current.Components["aegishub"]; !ok {
		t.Error("aegishub not found in reloaded manifest")
	}
}

