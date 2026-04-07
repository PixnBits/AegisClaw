package sandbox

import (
	"testing"
)

func TestNewOrchestrator_UnknownMode(t *testing.T) {
	_, err := NewOrchestrator("invalid", nil)
	if err == nil {
		t.Fatal("expected error for unknown isolation mode")
	}
}

func TestNewOrchestrator_FirecrackerRequiresRuntime(t *testing.T) {
	_, err := NewOrchestrator(IsolationFirecracker, nil)
	if err == nil {
		t.Fatal("expected error when FirecrackerRuntime is nil")
	}
}

func TestNewOrchestrator_DefaultModeRequiresRuntime(t *testing.T) {
	_, err := NewOrchestrator("", nil)
	if err == nil {
		t.Fatal("expected error when FirecrackerRuntime is nil for default mode")
	}
}

func TestNewOrchestrator_DockerUnsupported(t *testing.T) {
	_, err := NewOrchestrator("docker", nil)
	if err == nil {
		t.Fatal("expected error for docker mode (not supported on this platform)")
	}
}

func TestIsolationFirecrackerConstant(t *testing.T) {
	if IsolationFirecracker != "firecracker" {
		t.Errorf("expected IsolationFirecracker=%q, got %q", "firecracker", IsolationFirecracker)
	}
}
