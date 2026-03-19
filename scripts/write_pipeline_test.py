#!/usr/bin/env python3
"""Writes internal/builder/pipeline_test.go — unit tests for Pipeline orchestrator."""
import os

code = r'''package builder

import (
	"testing"
	"time"
)

func TestPipelineResultStates(t *testing.T) {
	states := []PipelineState{
		PipelineStatePending,
		PipelineStateBuilding,
		PipelineStateComplete,
		PipelineStateFailed,
		PipelineStateCancelled,
	}
	seen := make(map[PipelineState]bool)
	for _, s := range states {
		if seen[s] {
			t.Errorf("duplicate state: %s", s)
		}
		seen[s] = true
		if s == "" {
			t.Error("empty state string")
		}
	}
}

func TestNewPipelineValidation(t *testing.T) {
	tests := []struct {
		name    string
		wantErr string
	}{
		{"nil builder runtime", "builder runtime is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPipeline(nil, nil, nil, nil, nil, nil)
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tt.wantErr)
			} else if !containsStr(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestComputeFileHashes(t *testing.T) {
	files := map[string]string{
		"main.go":  "package main\n",
		"hello.go": "package hello\n",
	}

	hashes := computeFileHashes(files)

	if len(hashes) != 2 {
		t.Fatalf("expected 2 hashes, got %d", len(hashes))
	}

	for path, hash := range hashes {
		if hash == "" {
			t.Errorf("empty hash for %s", path)
		}
		if len(hash) != 64 {
			t.Errorf("expected 64-char SHA-256 hex for %s, got %d chars", path, len(hash))
		}
	}

	// Same content should produce same hash
	if hashes["main.go"] == hashes["hello.go"] {
		t.Error("different content should produce different hashes")
	}
}

func TestPipelineResultFields(t *testing.T) {
	result := &PipelineResult{
		ProposalID:  "test-123",
		State:       PipelineStateComplete,
		BuilderID:   "builder-456",
		CommitHash:  "abc123",
		Branch:      "proposal-test-123",
		Files:       map[string]string{"main.go": "package main"},
		FileHashes:  map[string]string{"main.go": "deadbeef"},
		Reasoning:   "test reasoning",
		Round:       1,
		StartedAt:   time.Now().UTC(),
		CompletedAt: time.Now().UTC(),
		Duration:    5 * time.Second,
	}

	if result.ProposalID != "test-123" {
		t.Errorf("unexpected proposal ID: %s", result.ProposalID)
	}
	if result.State != PipelineStateComplete {
		t.Errorf("unexpected state: %s", result.State)
	}
	if result.Branch != "proposal-test-123" {
		t.Errorf("unexpected branch: %s", result.Branch)
	}
	if len(result.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(result.Files))
	}
}
'''

outpath = os.path.join(os.path.dirname(__file__), '..', 'internal', 'builder', 'pipeline_test.go')
outpath = os.path.abspath(outpath)
with open(outpath, 'w') as f:
    f.write(code)
print(f"pipeline_test.go: {len(code)} bytes -> {outpath}")
