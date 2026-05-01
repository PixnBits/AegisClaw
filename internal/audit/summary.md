# `internal/audit/` — Package Summary

## Overview
Package `audit` provides two complementary audit-logging subsystems for AegisClaw:

1. **Merkle chain log** (`merkle.go`) — a tamper-evident, Ed25519-signed, append-only JSONL log used by the kernel to record every significant daemon action. Any insertion, deletion, or payload mutation in the file is detected by `VerifyChain`.

2. **Session log** (`session.go`) — a per-conversation JSONL log for the D2 chat subsystem that records every user message, assistant reply, tool call, and slash command with strict file permissions (`0600`) and directory permissions (`0700`).

Both formats use JSONL (one JSON object per line) with fsync after every write for durability.

## File Table

| File | Role |
|------|------|
| `helpers_test.go` | Shared `testLogger` helper for the test suite |
| `merkle.go` | `MerkleEntry`, `MerkleLog`, `VerifyChain`, `ReadEntries` |
| `merkle_test.go` | Append, chain continuation, tamper detection, wrong-key tests |
| `session.go` | `SessionEvent`, `SessionEventType`, `SessionLog` |
| `session_test.go` | File creation, event ordering, permissions, error-field tests |

## Key Interfaces & Types
- `MerkleLog` — thread-safe append-only chain log with Ed25519 signing
- `VerifyChain(path, pubKey)` — offline integrity verifier
- `SessionLog` — per-session chat event recorder
- `SessionEventType` — typed enum of chat events

## Notable Dependencies
- `crypto/ed25519`, `crypto/sha256` — cryptographic primitives
- `github.com/google/uuid` — unique IDs
- `go.uber.org/zap` — structured logging
