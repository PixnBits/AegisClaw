# registry_test.go

## Purpose
Tests for the sandbox `SkillRegistry` — its CRUD operations, Merkle hash chain integrity, root hash determinism, and cold-boot integrity repair. Tests use temporary directories and verify that the registry correctly persists state across store restarts.

## Key Types and Functions
- `TestRegister`: registers a skill entry and verifies it appears in `List` and `Get`
- `TestDeactivate`: registers then deactivates a skill; verifies state change is persisted
- `TestSetError`: verifies error state is recorded and retrievable
- `TestRootHash_Deterministic`: registers multiple skills in different orders and verifies the root hash is the same (sorted by name)
- `TestHashChain`: registers multiple entries and verifies each entry's `PrevHash` matches the previous entry's `MerkleHash`
- `TestRepairIntegrity`: manually corrupts a registry JSON file on disk and verifies `repairIntegrity` on reload detects and logs the corruption
- `TestSequence`: verifies the sequence counter increments monotonically with each mutation

## Role in the System
Ensures the skill registry reliably tracks deployed skills and maintains the tamper-evident hash chain that the kernel audit system depends on for integrity verification.

## Dependencies
- `testing`, `t.TempDir()`
- `encoding/json`: direct file manipulation for corruption test
- `internal/sandbox`: `SkillRegistry`, `SkillEntry`
