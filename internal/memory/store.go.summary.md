# store.go

## Purpose
Implements an age-encrypted, append-only JSONL vault for storing agent memories across sessions. Each memory entry is a structured record with a UUID, semantic key/value pair, tags, TTL tier, and security level. The vault file (`memory.vault.jsonl.age`) is re-encrypted on every write. An in-memory index is rebuilt at startup by decrypting and replaying all non-deleted entries. Compaction progressively truncates older values based on TTL tier to bound storage growth.

## Key Types and Functions
- `MemoryEntry`: MemoryID (UUID), Key, Value, Tags, TTLTier (90d/180d/365d/2yr/forever), SecurityLevel (low/medium/high), TaskID, Version, Deleted bool
- `Store`: central handle with in-memory index and vault path
- `NewStore(dir string, identity *age.X25519Identity) (*Store, error)`: opens/creates vault; decrypts and replays existing entries
- `Store(ctx, entry MemoryEntry) error`: scrubs PII then appends to vault
- `Retrieve(ctx, query string, topN int) ([]MemoryEntry, error)`: keyword-based retrieval
- `Get(ctx, id string) (*MemoryEntry, error)`: lookup by UUID
- `List(ctx) ([]MemoryEntry, error)`: all live entries
- `Delete(ctx, id string) error`: soft-delete via tombstone record
- `Compact(ctx) error`: truncates values by tier; `CompactAll(ctx)`: full compaction pass
- `Count(ctx) int`: live entry count

## Role in the System
Provides long-term agent memory with privacy guarantees. Used by the main agent loop to persist and retrieve task context, observations, and facts across sessions. PII scrubbing and age encryption ensure compliance with data-minimisation principles.

## Dependencies
- `filippo.io/age`: encryption/decryption
- `encoding/json`: JSONL serialisation
- `sync`: `RWMutex` for thread safety
- `internal/memory`: `Scrubber` for PII scrubbing
