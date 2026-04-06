// Package sandbox – orchestrator.go
//
// SandboxOrchestrator is the isolation-backend abstraction introduced by the
// OpenClaw integration plan (Phase 3, Task 8).  It decouples the rest of
// AegisClaw from the concrete Firecracker jailer, making it possible to add
// a Docker backend (or any other OCI-compatible runtime) without changing
// callers.
//
// Current backends:
//   - "firecracker" (default, production): wraps FirecrackerRuntime.
//   - "docker" (stub): returns ErrDockerNotImplemented; reserved for the
//     upcoming Docker sandbox backend.
//
// Use NewOrchestrator to construct the right backend from a config string.
package sandbox

import (
	"context"
	"errors"
	"fmt"
)

// ErrDockerNotImplemented is returned by the Docker stub backend.
// It signals that the Docker sandbox backend has not yet been implemented and
// that the caller should fall back to Firecracker or surface a user-visible
// error.
var ErrDockerNotImplemented = errors.New("docker sandbox backend is not yet implemented; use isolation_mode=firecracker")

// IsolationMode selects which container/VM backend to use.
type IsolationMode string

const (
	// IsolationFirecracker uses the existing Firecracker jailer (default,
	// highest isolation — hardware-virtualised microVMs).
	IsolationFirecracker IsolationMode = "firecracker"

	// IsolationDocker uses Docker with seccomp + AppArmor profiles
	// (primary target on Linux once the backend is complete — simpler setup,
	// no KVM requirement).
	IsolationDocker IsolationMode = "docker"
)

// Orchestrator is the common interface through which AegisClaw launches,
// monitors, and terminates sandboxed workloads, regardless of the underlying
// isolation technology.
//
// All implementations must:
//   - Sign and log every operation through the kernel before side-effects.
//   - Enforce network policies and capability restrictions declared in the spec.
//   - Never bypass the Governance Court approval gate.
type Orchestrator interface {
	// Mode returns the isolation backend name (e.g. "firecracker", "docker").
	Mode() IsolationMode

	// LaunchSandbox creates and immediately starts a sandbox from the given spec.
	// Returns the runtime ID that can be used with subsequent calls.
	LaunchSandbox(ctx context.Context, spec SandboxSpec) (id string, err error)

	// StopSandbox gracefully shuts down the sandbox identified by id.
	StopSandbox(ctx context.Context, id string) error

	// DeleteSandbox stops (if running) and removes all resources for id.
	DeleteSandbox(ctx context.Context, id string) error

	// SandboxStatus returns the current runtime info for the given id.
	SandboxStatus(ctx context.Context, id string) (*SandboxInfo, error)

	// ListSandboxes returns info for every known sandbox.
	ListSandboxes(ctx context.Context) ([]SandboxInfo, error)

	// SendToSandbox sends a JSON request to the guest agent running inside
	// the sandbox and returns the raw JSON response.
	SendToSandbox(ctx context.Context, id string, req interface{}) ([]byte, error)
}

// NewOrchestrator returns the Orchestrator implementation for the requested
// isolation mode.
//
//   - "firecracker" wraps the supplied FirecrackerRuntime (must be non-nil).
//   - "docker"      returns an ErrDockerNotImplemented stub.
//   - ""            defaults to "firecracker".
func NewOrchestrator(mode IsolationMode, fc *FirecrackerRuntime) (Orchestrator, error) {
	switch mode {
	case IsolationFirecracker, "":
		if fc == nil {
			return nil, fmt.Errorf("orchestrator: FirecrackerRuntime is required for isolation_mode=%q", IsolationFirecracker)
		}
		return &firecrackerOrchestrator{rt: fc}, nil
	case IsolationDocker:
		return &dockerOrchestratorStub{}, nil
	default:
		return nil, fmt.Errorf("orchestrator: unknown isolation_mode %q (supported: firecracker, docker)", mode)
	}
}

// ─── Firecracker backend ───────────────────────────────────────────────────

// firecrackerOrchestrator delegates to the existing FirecrackerRuntime so that
// all callers that upgrade to using the Orchestrator interface get exactly the
// same behaviour as before.
type firecrackerOrchestrator struct {
	rt *FirecrackerRuntime
}

func (o *firecrackerOrchestrator) Mode() IsolationMode { return IsolationFirecracker }

func (o *firecrackerOrchestrator) LaunchSandbox(ctx context.Context, spec SandboxSpec) (string, error) {
	if err := o.rt.Create(ctx, spec); err != nil {
		return "", fmt.Errorf("firecracker orchestrator: create: %w", err)
	}
	if err := o.rt.Start(ctx, spec.ID); err != nil {
		// Best-effort cleanup: delete the created-but-failed sandbox.
		// Log the delete error as a wrapped note so the primary start error
		// is still preserved for the caller.
		if delErr := o.rt.Delete(ctx, spec.ID); delErr != nil {
			return "", fmt.Errorf("firecracker orchestrator: start: %w (cleanup also failed: %v)", err, delErr)
		}
		return "", fmt.Errorf("firecracker orchestrator: start: %w", err)
	}
	return spec.ID, nil
}

func (o *firecrackerOrchestrator) StopSandbox(ctx context.Context, id string) error {
	return o.rt.Stop(ctx, id)
}

func (o *firecrackerOrchestrator) DeleteSandbox(ctx context.Context, id string) error {
	return o.rt.Delete(ctx, id)
}

func (o *firecrackerOrchestrator) SandboxStatus(ctx context.Context, id string) (*SandboxInfo, error) {
	return o.rt.Status(ctx, id)
}

func (o *firecrackerOrchestrator) ListSandboxes(ctx context.Context) ([]SandboxInfo, error) {
	return o.rt.List(ctx)
}

func (o *firecrackerOrchestrator) SendToSandbox(ctx context.Context, id string, req interface{}) ([]byte, error) {
	raw, err := o.rt.SendToVM(ctx, id, req)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// ─── Docker stub backend ───────────────────────────────────────────────────

// dockerOrchestratorStub satisfies the Orchestrator interface but returns
// ErrDockerNotImplemented for every operation.  It exists so that config
// validation can detect an unsupported mode early (at daemon start) rather
// than crashing when the first sandbox operation is attempted.
type dockerOrchestratorStub struct{}

func (d *dockerOrchestratorStub) Mode() IsolationMode { return IsolationDocker }

func (d *dockerOrchestratorStub) LaunchSandbox(_ context.Context, _ SandboxSpec) (string, error) {
	return "", ErrDockerNotImplemented
}

func (d *dockerOrchestratorStub) StopSandbox(_ context.Context, _ string) error {
	return ErrDockerNotImplemented
}

func (d *dockerOrchestratorStub) DeleteSandbox(_ context.Context, _ string) error {
	return ErrDockerNotImplemented
}

func (d *dockerOrchestratorStub) SandboxStatus(_ context.Context, _ string) (*SandboxInfo, error) {
	return nil, ErrDockerNotImplemented
}

func (d *dockerOrchestratorStub) ListSandboxes(_ context.Context) ([]SandboxInfo, error) {
	return nil, ErrDockerNotImplemented
}

func (d *dockerOrchestratorStub) SendToSandbox(_ context.Context, _ string, _ interface{}) ([]byte, error) {
	return nil, ErrDockerNotImplemented
}
