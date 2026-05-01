# Package: memory

## Overview
The `memory` package provides a secure, long-term agent memory store backed by an age-encrypted append-only JSONL vault. Memories are keyed by UUID and carry semantic key/value pairs, TTL tiers, and security classification levels. A built-in PII scrubber redacts sensitive data before any entry is persisted. On startup the vault is decrypted and replayed into an in-memory index, enabling fast retrieval without repeated decryption.

## Files
- `pii.go`: PII scrubber with regexp rules for email, phone, SSN, IPv4, JWT, AWS key, and generic secrets
- `pii_test.go`: Unit tests for each scrubbing pattern type
- `store.go`: age-encrypted JSONL vault with in-memory index, TTL tiers, compaction, and full CRUD API
- `store_test.go`: Integration tests for all store operations using a real age identity

## Key Abstractions
- `MemoryEntry`: structured record with UUID, semantic key/value, tags, TTL tier (90d → forever), security level (low/medium/high), task ID, version, and soft-delete flag
- `Store`: thread-safe vault handle; all writes go through PII scrubbing before encryption
- `Scrubber`: regexp-based redaction engine applied to both key and value fields
- TTL-based compaction: progressive value truncation (500→200→100→60 chars per tier) to bound vault size

## System Role
The memory store is used by the main agent orchestration layer to persist facts, observations, and task context across sessions. It acts as the agent's long-term episodic memory. The combination of age encryption and PII scrubbing ensures that even if the vault file is accessed outside the system, sensitive user data cannot be recovered.

## Dependencies
- `filippo.io/age`: envelope encryption for the vault file
- `encoding/json`: JSONL record format
- `sync`: `RWMutex` for concurrent access
- `github.com/google/uuid`: UUID generation for memory IDs
