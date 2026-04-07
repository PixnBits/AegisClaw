// Package sandbox – orchestrator.go
//
// Orchestrator is the isolation-backend abstraction introduced by the OpenClaw
// integration plan (Phase 3, Task 8).  It decouples the rest of AegisClaw from
// the concrete Firecracker jailer so that new backends (e.g. a future
// OCI/container runtime) can be added without changing callers.
//
// Supported backend:
//   - "firecracker" (default, only supported backend): wraps FirecrackerRuntime.
//     Hardware-virtualised microVMs via Firecracker + jailer.
//
// Passing an unsupported mode to NewOrchestrator returns an error immediately
// so the daemon fails fast at startup rather than at the first sandbox
// operation.
//
// Use NewOrchestrator to construct the right backend from a config string.
package sandbox

import (
	"context"
	"fmt"
)

// IsolationMode selects the VM/container backend for sandboxed workloads.
type IsolationMode string

const (
	// IsolationFirecracker uses the Firecracker jailer (default, only currently
	// supported backend on Linux).  Each workload runs in a hardware-virtualised
	// microVM with a read-only rootfs, capability dropping, and network-policy
	// enforcement via nftables.
	IsolationFirecracker IsolationMode = "firecracker"
)

// Orchestrator is the common interface through which AegisClaw launches,
// monitors, and terminates sandboxed workloads.
//
// All implementations must:
//   - Sign and log every operation through the kernel before side-effects.
//   - Enforce network policies and capability restrictions declared in the spec.
//   - Never bypass the Governance Court approval gate.
type Orchestrator interface {
	// Mode returns the isolation backend name (e.g. "firecracker").
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

// NewOrchestrator returns the Orchestrator for the requested isolation mode.
//
//   - "firecracker" (or "") wraps the supplied FirecrackerRuntime (must be
//     non-nil).
//   - Any other value returns an error immediately so the daemon fails fast.
func NewOrchestrator(mode IsolationMode, fc *FirecrackerRuntime) (Orchestrator, error) {
	switch mode {
	case IsolationFirecracker, "":
		if fc == nil {
			return nil, fmt.Errorf("orchestrator: FirecrackerRuntime is required for isolation_mode=%q", IsolationFirecracker)
		}
		return &firecrackerOrchestrator{rt: fc}, nil
	default:
		return nil, fmt.Errorf("orchestrator: unsupported isolation_mode %q (only %q is supported on this platform)", mode, IsolationFirecracker)
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
		// Wrap any delete error so the primary start error is still visible.
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
