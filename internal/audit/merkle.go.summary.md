# `merkle.go` — Tamper-Evident Audit Log

## Purpose
Implements an append-only, cryptographically chained audit log backed by a JSONL file. Every entry is linked to its predecessor via a SHA-256 hash and signed with the kernel's Ed25519 private key, making insertion, deletion, or modification of records detectable.

## Key Types / Functions

| Symbol | Description |
|--------|-------------|
| `MerkleEntry` | A single log record: `ID`, `PrevHash`, `Hash`, `Timestamp`, `Payload`, `Signature`. |
| `computeHash` | Deterministic SHA-256 over `ID + PrevHash + Timestamp + Payload`. |
| `MerkleEntry.Verify()` | Recomputes the hash and compares it to the stored value. |
| `MerkleEntry.VerifySignature(pubKey)` | Checks the Ed25519 signature against the entry's hash bytes. |
| `MerkleLog` | Wraps an `*os.File` (JSONL, `O_APPEND`), tracks `lastHash` and `entryCount`, and serialises appends under a mutex. |
| `NewMerkleLog` | Opens or creates the log file; calls `recoverChainState` to resume the chain after restarts. |
| `MerkleLog.Append(payload)` | Creates, signs, and fsyncs a new entry; returns `(id, hash, error)`. |
| `ReadEntries` | Reads all entries from a path using a 1 MB buffered scanner. |
| `VerifyChain` | Full chain verification: hash integrity + `PrevHash` linkage + Ed25519 signature for every entry. |

## How It Fits Into the Broader System
`MerkleLog` is the low-level immutable record store used by the kernel's `SignAndLog` path to produce a tamper-evident audit trail of every significant daemon action. The `VerifyChain` function can be called offline to prove log integrity.

## Notable Dependencies
- `crypto/ed25519`, `crypto/sha256` — signing and hashing.
- `github.com/google/uuid` — unique entry IDs.
- `go.uber.org/zap` — structured logging.
