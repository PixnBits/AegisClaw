package sandbox

import "context"

// SandboxManager defines the lifecycle interface for managing Firecracker sandboxes.
// All implementations must ensure that every operation is signed and logged
// through the kernel before side-effects occur.
type SandboxManager interface {
	// Create provisions a new sandbox from the given spec without starting it.
	Create(ctx context.Context, spec SandboxSpec) error

	// Start boots a previously created sandbox's Firecracker microVM.
	Start(ctx context.Context, id string) error

	// Stop gracefully shuts down a running sandbox's microVM.
	Stop(ctx context.Context, id string) error

	// Delete removes a stopped sandbox and cleans up all its resources.
	Delete(ctx context.Context, id string) error

	// List returns info for all known sandboxes regardless of state.
	List(ctx context.Context) ([]SandboxInfo, error)

	// Status returns the current info for a single sandbox.
	Status(ctx context.Context, id string) (*SandboxInfo, error)
}
