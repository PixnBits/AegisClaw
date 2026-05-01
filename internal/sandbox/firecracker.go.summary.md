# firecracker.go

## Purpose
Implements the `FirecrackerRuntime` which manages the full lifecycle of Firecracker microVM sandboxes on the host. It creates, starts, stops, deletes, and communicates with VMs using the firecracker-go-sdk. Each VM gets a dedicated TAP network device, a `/30` subnet, a vsock socket for guest communication, and a copy-on-write overlay of the root filesystem template. The runtime enforces seccomp level 2 and requires all configured paths to be absolute.

## Key Types and Functions
- `FirecrackerRuntime`: fields: `cfg RuntimeConfig`, kernel reference, logger, `sandboxes map[string]*managedSandbox`, `mu sync.RWMutex`, `nextCID uint32`
- `RuntimeConfig`: `FirecrackerBin`, `JailerBin`, `KernelImage`, `RootfsTemplate`, `ChrootBaseDir`, `StateDir` — all must be absolute paths
- `NewFirecrackerRuntime(cfg, logger) (*FirecrackerRuntime, error)`: validates config and initialises the runtime
- `Create(ctx, SandboxSpec) error`: allocates CID, TAP device, IPs; writes machine config
- `Start(ctx, id) error`: launches Firecracker process; waits for vsock socket to appear
- `Stop(ctx, id) error`: sends shutdown command; cleans up process
- `Delete(ctx, id) error`: removes state directory and TAP device
- `SendToVM(ctx, id, payload) ([]byte, error)`: vsock request/response communication
- `managedSandbox`: `info SandboxInfo`, `machine *firecracker.Machine`, `cancel context.CancelFunc`

## Role in the System
The core VM execution engine. All sandboxed skill execution ultimately runs through `FirecrackerRuntime`. It is wrapped by the `Orchestrator` for higher-level lifecycle management.

## Dependencies
- `github.com/firecracker-microvm/firecracker-go-sdk`: VM configuration and process management
- `internal/kernel`: kernel audit logging
- `net`, `os`, `os/exec`, `sync`: TAP devices, process management, concurrency
