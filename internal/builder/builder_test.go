package builder

import (
	"testing"
	"time"
)

func TestDefaultBuilderSpec(t *testing.T) {
	proposalID := "abcdef12-3456-7890-abcd-ef1234567890"
	spec := DefaultBuilderSpec(proposalID)

	if spec.ID == "" {
		t.Error("expected non-empty ID")
	}
	if spec.Name != "builder-abcdef12" {
		t.Errorf("expected name builder-abcdef12, got %s", spec.Name)
	}
	if spec.VCPUs != 2 {
		t.Errorf("expected 2 VCPUs, got %d", spec.VCPUs)
	}
	if spec.MemoryMB != 1024 {
		t.Errorf("expected 1024 MB, got %d", spec.MemoryMB)
	}
	if spec.WorkspaceMB != 512 {
		t.Errorf("expected 512 MB workspace, got %d", spec.WorkspaceMB)
	}
	if spec.ProposalID != proposalID {
		t.Errorf("expected proposal ID %s, got %s", proposalID, spec.ProposalID)
	}
	if len(spec.AllowedHosts) != 1 || spec.AllowedHosts[0] != "127.0.0.1" {
		t.Errorf("expected AllowedHosts=[127.0.0.1], got %v", spec.AllowedHosts)
	}
	if len(spec.AllowedPorts) != 1 || spec.AllowedPorts[0] != 11434 {
		t.Errorf("expected AllowedPorts=[11434], got %v", spec.AllowedPorts)
	}
}

func TestBuilderSpecValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*BuilderSpec)
		wantErr string
	}{
		{
			name:    "valid default spec",
			modify:  func(s *BuilderSpec) {},
			wantErr: "",
		},
		{
			name:    "empty ID",
			modify:  func(s *BuilderSpec) { s.ID = "" },
			wantErr: "builder ID is required",
		},
		{
			name:    "empty name",
			modify:  func(s *BuilderSpec) { s.Name = "" },
			wantErr: "builder name is required",
		},
		{
			name:    "VCPUs too low",
			modify:  func(s *BuilderSpec) { s.VCPUs = 0 },
			wantErr: "builder VCPUs must be between 1 and 8",
		},
		{
			name:    "VCPUs too high",
			modify:  func(s *BuilderSpec) { s.VCPUs = 16 },
			wantErr: "builder VCPUs must be between 1 and 8",
		},
		{
			name:    "memory too low",
			modify:  func(s *BuilderSpec) { s.MemoryMB = 128 },
			wantErr: "builder memory must be between 256 and 8192 MB",
		},
		{
			name:    "memory too high",
			modify:  func(s *BuilderSpec) { s.MemoryMB = 16384 },
			wantErr: "builder memory must be between 256 and 8192 MB",
		},
		{
			name:    "workspace too small",
			modify:  func(s *BuilderSpec) { s.WorkspaceMB = 32 },
			wantErr: "workspace must be between 64 and 4096 MB",
		},
		{
			name:    "workspace too large",
			modify:  func(s *BuilderSpec) { s.WorkspaceMB = 8192 },
			wantErr: "workspace must be between 64 and 4096 MB",
		},
		{
			name:    "empty proposal ID",
			modify:  func(s *BuilderSpec) { s.ProposalID = "" },
			wantErr: "proposal ID is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := DefaultBuilderSpec("abcdef12-3456-7890-abcd-ef1234567890")
			tt.modify(spec)
			err := spec.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !containsStr(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestDefaultBuilderConfig(t *testing.T) {
	cfg := DefaultBuilderConfig()

	if cfg.RootfsTemplate != "/var/lib/aegisclaw/rootfs-templates/builder.ext4" {
		t.Errorf("unexpected rootfs template: %s", cfg.RootfsTemplate)
	}
	if cfg.WorkspaceBaseDir != "/var/lib/aegisclaw/workspaces" {
		t.Errorf("unexpected workspace base dir: %s", cfg.WorkspaceBaseDir)
	}
	if cfg.MaxConcurrentBuilds != 2 {
		t.Errorf("expected 2 max concurrent builds, got %d", cfg.MaxConcurrentBuilds)
	}
	if cfg.BuildTimeout != 10*time.Minute {
		t.Errorf("expected 10m timeout, got %s", cfg.BuildTimeout)
	}
}

func TestBuilderConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*BuilderConfig)
		wantErr string
	}{
		{
			name:    "valid default config",
			modify:  func(c *BuilderConfig) {},
			wantErr: "",
		},
		{
			name:    "empty rootfs template",
			modify:  func(c *BuilderConfig) { c.RootfsTemplate = "" },
			wantErr: "builder rootfs template is required",
		},
		{
			name:    "empty workspace dir",
			modify:  func(c *BuilderConfig) { c.WorkspaceBaseDir = "" },
			wantErr: "workspace base directory is required",
		},
		{
			name:    "max builds too low",
			modify:  func(c *BuilderConfig) { c.MaxConcurrentBuilds = 0 },
			wantErr: "max concurrent builds must be between 1 and 8",
		},
		{
			name:    "max builds too high",
			modify:  func(c *BuilderConfig) { c.MaxConcurrentBuilds = 10 },
			wantErr: "max concurrent builds must be between 1 and 8",
		},
		{
			name:    "timeout too short",
			modify:  func(c *BuilderConfig) { c.BuildTimeout = 30 * time.Second },
			wantErr: "build timeout must be between 1 and 60 minutes",
		},
		{
			name:    "timeout too long",
			modify:  func(c *BuilderConfig) { c.BuildTimeout = 120 * time.Minute },
			wantErr: "build timeout must be between 1 and 60 minutes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultBuilderConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !containsStr(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestBuilderSpecToSandboxSpec(t *testing.T) {
	spec := DefaultBuilderSpec("abcdef12-3456-7890-abcd-ef1234567890")
	spec.RootfsPath = "/var/lib/aegisclaw/rootfs-templates/builder.ext4"

	sandboxSpec := spec.toSandboxSpec()

	if sandboxSpec.ID != spec.ID {
		t.Errorf("sandbox ID mismatch: %s != %s", sandboxSpec.ID, spec.ID)
	}
	if sandboxSpec.Name != spec.Name {
		t.Errorf("sandbox name mismatch: %s != %s", sandboxSpec.Name, spec.Name)
	}
	if sandboxSpec.Resources.VCPUs != spec.VCPUs {
		t.Errorf("VCPUs mismatch: %d != %d", sandboxSpec.Resources.VCPUs, spec.VCPUs)
	}
	if sandboxSpec.Resources.MemoryMB != spec.MemoryMB {
		t.Errorf("memory mismatch: %d != %d", sandboxSpec.Resources.MemoryMB, spec.MemoryMB)
	}
	if !sandboxSpec.NetworkPolicy.DefaultDeny {
		t.Error("expected DefaultDeny to be true")
	}
	if sandboxSpec.RootfsPath != spec.RootfsPath {
		t.Errorf("rootfs mismatch: %s != %s", sandboxSpec.RootfsPath, spec.RootfsPath)
	}
	if sandboxSpec.WorkspaceMB != spec.WorkspaceMB {
		t.Errorf("workspace mismatch: %d != %d", sandboxSpec.WorkspaceMB, spec.WorkspaceMB)
	}
}

func TestNewBuilderRuntimeValidation(t *testing.T) {
	cfg := DefaultBuilderConfig()

	_, err := NewBuilderRuntime(cfg, nil, nil, nil)
	if err == nil {
		t.Error("expected error for nil runtime")
	}
}

func TestBuilderRuntimeInvalidConfig(t *testing.T) {
	cfg := BuilderConfig{} // empty, invalid
	_, err := NewBuilderRuntime(cfg, nil, nil, nil)
	if err == nil {
		t.Error("expected error for invalid config")
	}
}

func TestBuilderStateConstants(t *testing.T) {
	states := []BuilderState{
		BuilderStateIdle,
		BuilderStateBuilding,
		BuilderStateStopped,
		BuilderStateError,
	}
	seen := make(map[BuilderState]bool)
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

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
