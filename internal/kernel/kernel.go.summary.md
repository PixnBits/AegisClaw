# `kernel.go` — Singleton Kernel

## Purpose
Implements the immutable singleton kernel — the single point of authority for signing and audit-logging all operations in AegisClaw. It manages an Ed25519 keypair, the append-only Merkle audit log, and the vsock `ControlPlane`.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `Kernel` | Holds `ed25519.PrivateKey`, `ed25519.PublicKey`, `*audit.MerkleLog`, `*ControlPlane` |
| `GetInstance(logger, auditDir)` | Returns the singleton, initializing on first call via `sync.Once` |
| `ResetInstance()` | Tears down the singleton for test isolation |
| `Kernel.SignAndLog(action)` | Signs the action with Ed25519, appends to the Merkle chain, returns `*SignedAction` |
| `Kernel.Sign(data)` | Signs arbitrary bytes (used by the vault) |
| `Kernel.Verify(data, sig)` | Verifies an Ed25519 signature against the kernel's public key |
| `Kernel.PrivateKeyBytes()` | Returns raw private key (used by the vault to derive an age identity) |
| `Kernel.PublicKey()` | Returns the kernel's Ed25519 public key |
| `Kernel.ControlPlane()` | Returns the vsock control plane |
| `Kernel.Shutdown()` | Gracefully shuts down the control plane |
| `loadOrGenerateKey(logger, keyDir)` | Reads `~/.local/share/aegisclaw/kernel/kernel.key` or generates a new keypair; stored with `0600` permissions |

## Security Properties
- Singleton pattern (`sync.Once`) prevents multiple kernel instances per process.
- All actions are signed and fsynced to the Merkle log before `SignAndLog` returns.
- Key file stored at `0600`; generated with `crypto/rand`.

## Role in the System
Everything flows through the kernel. It is initialized first at daemon startup and referenced by the court engine, IPC bridge, event bus, composition store, and every tool handler that needs audit logging.

## Notable Dependencies
- `internal/audit` (MerkleLog)
- Standard library: `crypto/ed25519`, `crypto/rand`, `sync`
- `go.uber.org/zap`
