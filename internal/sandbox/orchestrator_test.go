package sandbox

import (
	"testing"
)

func TestNewOrchestrator_UnknownMode(t *testing.T) {
	_, err := NewOrchestrator("invalid", nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown isolation mode")
	}
}

func TestNewOrchestrator_FirecrackerRequiresRuntime(t *testing.T) {
	_, err := NewOrchestrator(IsolationFirecracker, nil, nil)
	if err == nil {
		t.Fatal("expected error when FirecrackerRuntime is nil")
	}
}

func TestNewOrchestrator_DefaultModeRequiresRuntime(t *testing.T) {
	_, err := NewOrchestrator("", nil, nil)
	if err == nil {
		t.Fatal("expected error when FirecrackerRuntime is nil for default mode")
	}
}

func TestNewOrchestrator_DockerRequiresRuntime(t *testing.T) {
	_, err := NewOrchestrator(IsolationDocker, nil, nil)
	if err == nil {
		t.Fatal("expected error when DockerRuntime is nil for docker mode")
	}
}

func TestIsolationFirecrackerConstant(t *testing.T) {
	if IsolationFirecracker != "firecracker" {
		t.Errorf("expected IsolationFirecracker=%q, got %q", "firecracker", IsolationFirecracker)
	}
}

func TestIsolationDockerConstant(t *testing.T) {
	if IsolationDocker != "docker" {
		t.Errorf("expected IsolationDocker=%q, got %q", "docker", IsolationDocker)
	}
}
