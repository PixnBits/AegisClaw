package sandbox

import (
	"context"
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

func TestNewOrchestrator_DockerStub(t *testing.T) {
	orch, err := NewOrchestrator(IsolationDocker, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if orch.Mode() != IsolationDocker {
		t.Errorf("expected mode %q, got %q", IsolationDocker, orch.Mode())
	}

	// All operations should return ErrDockerNotImplemented.
	ctx := context.Background()
	if _, err := orch.LaunchSandbox(ctx, SandboxSpec{}); err != ErrDockerNotImplemented {
		t.Errorf("expected ErrDockerNotImplemented from LaunchSandbox, got %v", err)
	}
	if err := orch.StopSandbox(ctx, "x"); err != ErrDockerNotImplemented {
		t.Errorf("expected ErrDockerNotImplemented from StopSandbox, got %v", err)
	}
	if err := orch.DeleteSandbox(ctx, "x"); err != ErrDockerNotImplemented {
		t.Errorf("expected ErrDockerNotImplemented from DeleteSandbox, got %v", err)
	}
	if _, err := orch.SandboxStatus(ctx, "x"); err != ErrDockerNotImplemented {
		t.Errorf("expected ErrDockerNotImplemented from SandboxStatus, got %v", err)
	}
	if _, err := orch.ListSandboxes(ctx); err != ErrDockerNotImplemented {
		t.Errorf("expected ErrDockerNotImplemented from ListSandboxes, got %v", err)
	}
	if _, err := orch.SendToSandbox(ctx, "x", nil); err != ErrDockerNotImplemented {
		t.Errorf("expected ErrDockerNotImplemented from SendToSandbox, got %v", err)
	}
}
