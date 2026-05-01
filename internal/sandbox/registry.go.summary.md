# registry.go

## Purpose
Implements a persistent, tamper-evident skill registry within the sandbox package. Tracks which skills are currently deployed into sandboxes, their lifecycle states, and forms a Merkle hash chain for integrity verification. The registry detects corruption on cold boot via `repairIntegrity` and maintains a deterministic root hash over all registered skill names for audit purposes.

## Key Types and Functions
- `SkillRegistry`: wraps a JSON file on disk
- `SkillEntry`: Name, SandboxID, State (pending/active/error/deactivated), MerkleHash, PrevHash, Version, Metadata
- `Register(ctx, SkillEntry) error`: adds or updates a skill entry; computes and links hash
- `Deactivate(ctx, name) error`: marks a skill as deactivated; updates hash chain
- `SetError(ctx, name, errMsg) error`: records an error state for a skill
- `Get(ctx, name) (*SkillEntry, error)`: retrieves a single entry
- `List(ctx) ([]SkillEntry, error)`: returns all entries
- `RootHash(ctx) string`: SHA-256 root hash over sorted skill names for determinism
- `Sequence(ctx) int`: monotonically increasing count of registry mutations
- `computeEntryHash(entry) string`: SHA-256 of entry fields; `computeRootHash(entries) string`: sorts names before hashing
- `repairIntegrity`: re-computes hashes on cold boot to detect file tampering

## Role in the System
Maintained by `FirecrackerRuntime` to track active skill deployments. The root hash and Merkle chain are included in kernel audit log entries, enabling end-to-end verification that the set of running skills matches governance approvals.

## Dependencies
- `crypto/sha256`, `encoding/json`: hashing and persistence
- `sync`: `RWMutex` for concurrent access
