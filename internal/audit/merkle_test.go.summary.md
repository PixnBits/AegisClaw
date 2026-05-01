# `merkle_test.go` — Merkle Log Tests

## Purpose
Comprehensive tests for `MerkleLog` and `VerifyChain`, covering the full lifecycle of the tamper-evident audit chain.

## Key Tests

| Test | What It Verifies |
|------|-----------------|
| `TestMerkleLog_AppendAndVerify` | Appends 10 entries, checks `EntryCount` and `LastHash`, then calls `VerifyChain` and asserts all 10 entries pass. |
| `TestMerkleLog_ChainContinuation` | Writes 5 entries, closes the log, reopens it, verifies chain state is recovered (`EntryCount == 5`, `LastHash` matches), appends 5 more, and verifies all 10. |
| `TestMerkleLog_TamperDetection` | Writes 5 entries, flips a byte in the third entry's payload on disk, and asserts `VerifyChain` returns an error. |
| `TestMerkleLog_WrongKeyDetection` | Writes an entry signed with one key pair, then calls `VerifyChain` with a different public key and expects failure. |
| `TestMerkleLog_EmptyLogVerifies` | Confirms `VerifyChain` returns `(0, nil)` for an empty file. |

## How It Fits Into the Broader System
These tests guard the security properties of the audit subsystem. Tamper detection and key-binding are critical invariants that protect the integrity of the kernel's action log.

## Notable Dependencies
- `crypto/ed25519`, `crypto/rand` for key generation.
- Standard library `os`, `path/filepath`, `testing`.
