# manager.go

## Purpose
Defines the `SandboxManager` interface — the low-level CRUD contract for managing individual sandbox VMs. This interface abstracts over the concrete Firecracker runtime, enabling alternative implementations (e.g., a mock manager for tests or a future Kata Containers backend) without changing higher-level orchestration code.

## Key Types and Functions
- `SandboxManager` interface: `Create(ctx, SandboxSpec) error`, `Start(ctx, id) error`, `Stop(ctx, id) error`, `Delete(ctx, id) error`, `List(ctx) ([]SandboxInfo, error)`, `Status(ctx, id) (*SandboxInfo, error)`
- All methods are context-aware for cancellation and deadline propagation
- The interface is implemented by `FirecrackerRuntime` in `runtime.go` (now `firecracker.go`)

## Role in the System
`SandboxManager` is the boundary between the orchestration layer and the VM runtime. The `Orchestrator` (defined in `orchestrator.go`) delegates all VM lifecycle operations to a `SandboxManager` implementation. This separation allows the orchestrator to be tested with a mock manager and production code to use Firecracker without the orchestrator knowing the difference.

## Dependencies
- `context`: all interface methods accept a context
- Standard library only; no external dependencies — pure interface file
