package sandbox

import (
	"testing"
)

// TestDockerRuntimeConfigValidate checks that DockerRuntimeConfig.Validate
// enforces the required fields.
func TestDockerRuntimeConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     DockerRuntimeConfig
		wantErr bool
	}{
		{
			name:    "missing StateDir",
			cfg:     DockerRuntimeConfig{},
			wantErr: true,
		},
		{
			name:    "relative StateDir",
			cfg:     DockerRuntimeConfig{StateDir: "relative/path"},
			wantErr: true,
		},
		{
			name:    "valid config",
			cfg:     DockerRuntimeConfig{StateDir: "/tmp/aegisclaw-docker-test"},
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestValidateDockerSpec checks that validateDockerSpec enforces the fields
// required by DockerRuntime without requiring a VsockCID or absolute RootfsPath.
func TestValidateDockerSpec(t *testing.T) {
	validSpec := SandboxSpec{
		ID:          "test-sandbox-001",
		Name:        "test-sandbox",
		DockerImage: "aegisclaw/guest:latest",
		Resources:   Resources{VCPUs: 1, MemoryMB: 256},
		NetworkPolicy: NetworkPolicy{
			NoNetwork:   true,
			DefaultDeny: true,
		},
	}

	if err := validateDockerSpec(&validSpec); err != nil {
		t.Fatalf("unexpected validation error for valid spec: %v", err)
	}

	// Missing DockerImage should fail.
	noImage := validSpec
	noImage.DockerImage = ""
	if err := validateDockerSpec(&noImage); err == nil {
		t.Error("expected error when DockerImage is empty")
	}

	// Missing ID should fail.
	noID := validSpec
	noID.ID = ""
	if err := validateDockerSpec(&noID); err == nil {
		t.Error("expected error when ID is empty")
	}

	// VCPUs out of range should fail.
	badCPU := validSpec
	badCPU.Resources.VCPUs = 0
	if err := validateDockerSpec(&badCPU); err == nil {
		t.Error("expected error when VCPUs is 0")
	}

	// DefaultDeny = false should fail.
	allowAll := validSpec
	allowAll.NetworkPolicy.DefaultDeny = false
	if err := validateDockerSpec(&allowAll); err == nil {
		t.Error("expected error when DefaultDeny is false")
	}
}

// TestStatePausedConstant verifies the StatePaused constant value.
func TestStatePausedConstant(t *testing.T) {
	if StatePaused != "paused" {
		t.Errorf("expected StatePaused=%q, got %q", "paused", StatePaused)
	}
}

// TestContainerName verifies the Docker container naming scheme.
func TestContainerName(t *testing.T) {
	if got := containerName("my-skill-abc123"); got != "aegis-my-skill-abc123" {
		t.Errorf("containerName() = %q, want %q", got, "aegis-my-skill-abc123")
	}
}

// TestIsNoSuchContainer verifies detection of Docker "no such container" output.
func TestIsNoSuchContainer(t *testing.T) {
	cases := []struct {
		out  string
		want bool
	}{
		{"Error response from daemon: No such container: aegis-abc\n", true},
		{"Error: No such container: foo", true},
		{"no such container: bar", true},
		{"Error: cannot stop container: timeout", false},
		{"", false},
	}
	for _, tc := range cases {
		got := isNoSuchContainer(tc.out)
		if got != tc.want {
			t.Errorf("isNoSuchContainer(%q) = %v, want %v", tc.out, got, tc.want)
		}
	}
}
