# Package `internal/kernel` — Singleton Kernel

## Purpose
The immutable cryptographic core of AegisClaw. Provides a singleton `Kernel` that owns an Ed25519 keypair, an append-only Merkle audit log, and the vsock `ControlPlane` for communicating with guest VMs. Every auditable operation in the system flows through `Kernel.SignAndLog`.

## Files

| File | Description |
|---|---|
| `action.go` | `ActionType` constants (~50), `Action`, `SignedAction`, `NewAction`, `Action.Validate`, `Action.Marshal` |
| `controlplane.go` | `ControlPlane` — vsock Unix socket management; `RegisterHandler`, `ListenForVM`, `Send`, `Shutdown` |
| `kernel.go` | `Kernel` singleton: `GetInstance`, `SignAndLog`, `Sign`, `Verify`, `ControlPlane()`, key load/generate |
| `kernel_test.go` | Tests for singleton lifecycle, signing, Merkle logging, key persistence, action validation |

## Key Abstractions

- **`Kernel`** — singleton (`sync.Once`); holds Ed25519 key pair; all subsystems share the same instance
- **`SignAndLog(action)`** — mandatory entry point: signs action bytes, appends to Merkle chain, returns `*SignedAction`; fsync'd before returning
- **`Action`** — audit record: UUID, `ActionType`, source, JSON payload, UTC timestamp
- **`ActionType`** — exhaustive enum covering sandbox, skill, proposal, builder, secret, memory, event bus, worker, and system component lifecycle events
- **`ControlPlane`** — listens on per-VM Unix sockets (Firecracker vsock UDS); dispatches typed `ControlMessage` packets to registered `MessageHandler` functions

## Security Properties
- Ed25519 key stored at `~/.local/share/aegisclaw/kernel/kernel.key` with `0600` permissions
- Generated with `crypto/rand`; no key derivation from passwords
- Every action is cryptographically signed and persisted to the Merkle audit chain before `SignAndLog` returns
- Singleton pattern prevents multiple keys or audit chains per process

## How It Fits Into the Broader System
Initialized first at daemon startup. Referenced by the court engine, IPC bridge, event bus, composition store, memory store, and every tool handler that writes audit records. The `ControlPlane` is the physical transport layer between the host daemon and all guest VMs.

## Notable Dependencies
- `internal/audit` (MerkleLog)
- Standard library: `crypto/ed25519`, `crypto/rand`, `net`, `sync`
- `go.uber.org/zap`, `github.com/google/uuid`
