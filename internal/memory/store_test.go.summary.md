# store_test.go

## Purpose
Integration tests for the memory `Store`. Uses a real age X25519 identity to exercise the full encryption/decryption cycle and validates all public methods of the store. Tests confirm that data persists correctly across store restarts, PII scrubbing is applied transparently, and compaction correctly truncates long values according to TTL tier rules.

## Key Types and Functions
- `TestNewStore`: verifies store creation, vault file creation, and empty initial state
- `TestStoreAndRetrieve`: stores entries and verifies keyword retrieval returns relevant results
- `TestGet`: stores an entry by UUID and retrieves it by ID
- `TestList`: stores multiple entries and verifies all appear in list output
- `TestDelete`: soft-deletes an entry and confirms it disappears from `List` and `Get`
- `TestCompact`: stores entries with long values and verifies compaction truncates them to tier limits
- `TestCompactAll`: exercises full-pass compaction across multiple TTL tiers
- `TestCount`: verifies count increments and decrements correctly with store/delete operations
- `TestPIIScrubbing`: stores an entry containing an email address and verifies `[EMAIL]` appears in the vault

## Role in the System
Provides end-to-end confidence in the memory subsystem, including the interaction between age encryption, PII scrubbing, JSONL appending, in-memory indexing, and compaction. Catches regressions in the vault's persistence and privacy guarantees.

## Dependencies
- `testing`, `t.TempDir()`
- `filippo.io/age`: key generation for test identity
- `internal/memory`: package under test
