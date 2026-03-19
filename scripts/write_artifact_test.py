#!/usr/bin/env python3
"""Writes internal/builder/artifact_test.go — Tests for artifact signing & packaging."""
import os

code = r'''package builder

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

func setupTestArtifactStore(t *testing.T) (*ArtifactStore, *kernel.Kernel, string) {
	t.Helper()
	kernel.ResetInstance()

	dir := t.TempDir()
	auditDir := filepath.Join(dir, "audit")
	artifactDir := filepath.Join(dir, "artifacts")

	logger := zap.NewNop()

	kern, err := kernel.GetInstance(logger, auditDir)
	if err != nil {
		t.Fatalf("failed to create kernel: %v", err)
	}

	store, err := NewArtifactStore(artifactDir, kern, logger)
	if err != nil {
		t.Fatalf("failed to create artifact store: %v", err)
	}

	return store, kern, artifactDir
}

func TestNewArtifactStoreValidation(t *testing.T) {
	logger := zap.NewNop()

	_, err := NewArtifactStore("", nil, logger)
	if err == nil {
		t.Fatal("expected error for empty base dir")
	}

	_, err = NewArtifactStore("/tmp/test-artifacts", nil, logger)
	if err == nil {
		t.Fatal("expected error for nil kernel")
	}
}

func TestPackageAndVerifyArtifact(t *testing.T) {
	store, _, artifactDir := setupTestArtifactStore(t)

	spec := &SkillSpec{
		Name:          "test-skill",
		Description:   "A test skill",
		Language:      "go",
		EntryPoint:    "main.go",
		Tools:         []ToolSpec{{Name: "test-tool", Description: "test"}},
		NetworkPolicy: SkillNetworkPolicy{DefaultDeny: true},
	}

	binaryData := []byte("fake-binary-data-for-testing")
	fileHashes := map[string]string{
		"main.go":  "abc123",
		"util.go":  "def456",
	}

	// Package
	manifest, err := store.PackageArtifact(
		"test-skill",
		"prop-123",
		"v1.0.0",
		"commit-abc",
		binaryData,
		fileHashes,
		spec,
	)
	if err != nil {
		t.Fatalf("PackageArtifact failed: %v", err)
	}

	// Verify manifest fields
	if manifest.SkillID != "test-skill" {
		t.Errorf("skill ID mismatch: %s", manifest.SkillID)
	}
	if manifest.ProposalID != "prop-123" {
		t.Errorf("proposal ID mismatch: %s", manifest.ProposalID)
	}
	if manifest.Version != "v1.0.0" {
		t.Errorf("version mismatch: %s", manifest.Version)
	}
	if manifest.CommitHash != "commit-abc" {
		t.Errorf("commit hash mismatch: %s", manifest.CommitHash)
	}
	if manifest.BinaryHash == "" {
		t.Error("binary hash should not be empty")
	}
	if manifest.BinarySize != int64(len(binaryData)) {
		t.Errorf("binary size mismatch: %d", manifest.BinarySize)
	}
	if manifest.Signature == "" {
		t.Error("signature should not be empty")
	}
	if manifest.BuildMode != "pie" {
		t.Errorf("build mode should be pie, got %s", manifest.BuildMode)
	}
	if manifest.KernelPubKey == "" {
		t.Error("kernel public key should not be empty")
	}

	// Verify sandbox manifest
	if manifest.Sandbox.VCPUs != 1 {
		t.Errorf("sandbox vCPUs mismatch: %d", manifest.Sandbox.VCPUs)
	}
	if manifest.Sandbox.MemoryMB != 256 {
		t.Errorf("sandbox memory mismatch: %d", manifest.Sandbox.MemoryMB)
	}
	if !manifest.Sandbox.ReadOnlyRoot {
		t.Error("sandbox should have read-only root")
	}
	if manifest.Sandbox.NetworkPolicy != "default-deny" {
		t.Errorf("network policy mismatch: %s", manifest.Sandbox.NetworkPolicy)
	}

	// Verify files exist on disk
	skillDir := filepath.Join(artifactDir, "test-skill")
	for _, fname := range []string{"skill", "manifest.json", "manifest.sig", "SHA256SUMS"} {
		fpath := filepath.Join(skillDir, fname)
		if _, err := os.Stat(fpath); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", fname)
		}
	}

	// Verify the artifact
	verified, err := store.VerifyArtifact("test-skill")
	if err != nil {
		t.Fatalf("VerifyArtifact failed: %v", err)
	}
	if verified.SkillID != "test-skill" {
		t.Errorf("verified skill ID mismatch: %s", verified.SkillID)
	}
	if verified.BinaryHash != manifest.BinaryHash {
		t.Errorf("verified hash mismatch")
	}
}

func TestPackageArtifactPathTraversal(t *testing.T) {
	store, _, _ := setupTestArtifactStore(t)

	spec := &SkillSpec{
		Name:          "test-skill",
		Description:   "test",
		Language:      "go",
		EntryPoint:    "main.go",
		Tools:         []ToolSpec{{Name: "t", Description: "t"}},
		NetworkPolicy: SkillNetworkPolicy{DefaultDeny: true},
	}

	badIDs := []string{"../etc/passwd", "foo/bar", "/absolute", ".."}
	for _, id := range badIDs {
		_, err := store.PackageArtifact(id, "p-1", "v1", "c1", []byte("data"), nil, spec)
		if err == nil {
			t.Errorf("expected error for skill ID %q", id)
		}
	}
}

func TestPackageArtifactValidation(t *testing.T) {
	store, _, _ := setupTestArtifactStore(t)

	spec := &SkillSpec{
		Name:          "test-skill",
		Description:   "test",
		Language:      "go",
		EntryPoint:    "main.go",
		Tools:         []ToolSpec{{Name: "t", Description: "t"}},
		NetworkPolicy: SkillNetworkPolicy{DefaultDeny: true},
	}

	// Empty skill ID
	_, err := store.PackageArtifact("", "p-1", "v1", "c1", []byte("data"), nil, spec)
	if err == nil {
		t.Error("expected error for empty skill ID")
	}

	// Empty proposal ID
	_, err = store.PackageArtifact("skill1", "", "v1", "c1", []byte("data"), nil, spec)
	if err == nil {
		t.Error("expected error for empty proposal ID")
	}

	// Empty binary
	_, err = store.PackageArtifact("skill1", "p-1", "v1", "c1", nil, nil, spec)
	if err == nil {
		t.Error("expected error for nil binary")
	}

	// Nil spec
	_, err = store.PackageArtifact("skill1", "p-1", "v1", "c1", []byte("data"), nil, nil)
	if err == nil {
		t.Error("expected error for nil spec")
	}
}

func TestVerifyArtifactTamperedBinary(t *testing.T) {
	store, _, artifactDir := setupTestArtifactStore(t)

	spec := &SkillSpec{
		Name:          "tamper-test",
		Description:   "test",
		Language:      "go",
		EntryPoint:    "main.go",
		Tools:         []ToolSpec{{Name: "t", Description: "t"}},
		NetworkPolicy: SkillNetworkPolicy{DefaultDeny: true},
	}

	_, err := store.PackageArtifact("tamper-test", "p-1", "v1", "c1", []byte("original"), nil, spec)
	if err != nil {
		t.Fatalf("PackageArtifact failed: %v", err)
	}

	// Tamper with the binary
	binaryPath := filepath.Join(artifactDir, "tamper-test", "skill")
	if err := os.WriteFile(binaryPath, []byte("tampered"), 0o640); err != nil {
		t.Fatalf("failed to tamper binary: %v", err)
	}

	_, err = store.VerifyArtifact("tamper-test")
	if err == nil {
		t.Fatal("expected verification to fail for tampered binary")
	}
	if !containsStr(err.Error(), "hash mismatch") {
		t.Errorf("expected hash mismatch error, got: %v", err)
	}
}

func TestVerifyArtifactPathTraversal(t *testing.T) {
	store, _, _ := setupTestArtifactStore(t)

	_, err := store.VerifyArtifact("../etc/passwd")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestListArtifacts(t *testing.T) {
	store, _, _ := setupTestArtifactStore(t)

	spec := &SkillSpec{
		Name:          "list-test",
		Description:   "test",
		Language:      "go",
		EntryPoint:    "main.go",
		Tools:         []ToolSpec{{Name: "t", Description: "t"}},
		NetworkPolicy: SkillNetworkPolicy{DefaultDeny: true},
	}

	// Initially empty
	skills, err := store.ListArtifacts()
	if err != nil {
		t.Fatalf("ListArtifacts failed: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 artifacts, got %d", len(skills))
	}

	// Add two artifacts
	_, err = store.PackageArtifact("skill-a", "p-1", "v1", "c1", []byte("bin-a"), nil, spec)
	if err != nil {
		t.Fatalf("failed to package skill-a: %v", err)
	}
	_, err = store.PackageArtifact("skill-b", "p-2", "v1", "c2", []byte("bin-b"), nil, spec)
	if err != nil {
		t.Fatalf("failed to package skill-b: %v", err)
	}

	skills, err = store.ListArtifacts()
	if err != nil {
		t.Fatalf("ListArtifacts failed: %v", err)
	}
	if len(skills) != 2 {
		t.Errorf("expected 2 artifacts, got %d", len(skills))
	}
}

func TestGetManifest(t *testing.T) {
	store, _, _ := setupTestArtifactStore(t)

	spec := &SkillSpec{
		Name:          "get-test",
		Description:   "test",
		Language:      "go",
		EntryPoint:    "main.go",
		Tools:         []ToolSpec{{Name: "t", Description: "t"}},
		NetworkPolicy: SkillNetworkPolicy{DefaultDeny: true},
	}

	_, err := store.PackageArtifact("get-test", "p-1", "v1.0.0", "c1", []byte("binary"), nil, spec)
	if err != nil {
		t.Fatalf("failed to package: %v", err)
	}

	manifest, err := store.GetManifest("get-test")
	if err != nil {
		t.Fatalf("GetManifest failed: %v", err)
	}
	if manifest.SkillID != "get-test" {
		t.Errorf("skill ID mismatch: %s", manifest.SkillID)
	}
	if manifest.Version != "v1.0.0" {
		t.Errorf("version mismatch: %s", manifest.Version)
	}
}

func TestGetManifestNotFound(t *testing.T) {
	store, _, _ := setupTestArtifactStore(t)

	_, err := store.GetManifest("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent skill")
	}
}

func TestGetManifestPathTraversal(t *testing.T) {
	store, _, _ := setupTestArtifactStore(t)

	_, err := store.GetManifest("../secrets")
	if err == nil {
		t.Error("expected error for path traversal")
	}
}

func TestArtifactManifestValidation(t *testing.T) {
	tests := []struct {
		name     string
		manifest *ArtifactManifest
		wantErr  string
	}{
		{
			"empty skill ID",
			&ArtifactManifest{ProposalID: "p", Version: "v1", BinaryHash: "h", BinarySize: 1, Signature: "s"},
			"skill ID is required",
		},
		{
			"empty proposal ID",
			&ArtifactManifest{SkillID: "s", Version: "v1", BinaryHash: "h", BinarySize: 1, Signature: "s"},
			"proposal ID is required",
		},
		{
			"empty version",
			&ArtifactManifest{SkillID: "s", ProposalID: "p", BinaryHash: "h", BinarySize: 1, Signature: "s"},
			"version is required",
		},
		{
			"empty binary hash",
			&ArtifactManifest{SkillID: "s", ProposalID: "p", Version: "v1", BinarySize: 1, Signature: "s"},
			"binary hash is required",
		},
		{
			"zero binary size",
			&ArtifactManifest{SkillID: "s", ProposalID: "p", Version: "v1", BinaryHash: "h", Signature: "s"},
			"binary size must be positive",
		},
		{
			"empty signature",
			&ArtifactManifest{SkillID: "s", ProposalID: "p", Version: "v1", BinaryHash: "h", BinarySize: 1},
			"signature is required",
		},
		{
			"valid manifest",
			&ArtifactManifest{SkillID: "s", ProposalID: "p", Version: "v1", BinaryHash: "h", BinarySize: 1, Signature: "s"},
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.manifest.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q", tt.wantErr)
				} else if !containsStr(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestArtifactManifestJSON(t *testing.T) {
	manifest := &ArtifactManifest{
		SkillID:    "test-skill",
		ProposalID: "p-1",
		Version:    "v1.0.0",
		CommitHash: "abc123",
		BinaryHash: "def456",
		BinarySize: 1024,
		Signature:  "sig789",
		FileHashes: map[string]string{"main.go": "hash1"},
		Sandbox: SandboxManifest{
			VCPUs:        1,
			MemoryMB:     256,
			ReadOnlyRoot: true,
			NetworkPolicy: "default-deny",
		},
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded ArtifactManifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.SkillID != "test-skill" || decoded.Version != "v1.0.0" {
		t.Errorf("roundtrip mismatch: %+v", decoded)
	}
	if decoded.Sandbox.VCPUs != 1 || !decoded.Sandbox.ReadOnlyRoot {
		t.Errorf("sandbox roundtrip mismatch: %+v", decoded.Sandbox)
	}
}

func TestFormatNetworkPolicy(t *testing.T) {
	tests := []struct {
		name   string
		policy SkillNetworkPolicy
		want   string
	}{
		{
			"default deny no hosts",
			SkillNetworkPolicy{DefaultDeny: true},
			"default-deny",
		},
		{
			"deny with allowed hosts",
			SkillNetworkPolicy{DefaultDeny: true, AllowedHosts: []string{"api.example.com", "db.local"}},
			"deny-except:api.example.com,db.local",
		},
		{
			"allow all",
			SkillNetworkPolicy{DefaultDeny: false},
			"allow-all",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatNetworkPolicy(tt.policy)
			if got != tt.want {
				t.Errorf("formatNetworkPolicy() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestArtifactTypeConstants(t *testing.T) {
	if ArtifactTypeBinary != "binary" {
		t.Errorf("ArtifactTypeBinary = %q", ArtifactTypeBinary)
	}
	if ArtifactTypeManifest != "manifest" {
		t.Errorf("ArtifactTypeManifest = %q", ArtifactTypeManifest)
	}
	if ArtifactTypeSource != "source" {
		t.Errorf("ArtifactTypeSource = %q", ArtifactTypeSource)
	}
}

func TestSha256Sum(t *testing.T) {
	data := []byte("hello world")
	sum := sha256Sum(data)
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	got := hex.EncodeToString(sum)
	if got != expected {
		t.Errorf("sha256Sum mismatch: got %s, want %s", got, expected)
	}
}
'''

outpath = os.path.join(os.path.dirname(__file__), '..', 'internal', 'builder', 'artifact_test.go')
outpath = os.path.abspath(outpath)
with open(outpath, 'w') as f:
    f.write(code)
print(f"artifact_test.go: {len(code)} bytes -> {outpath}")
