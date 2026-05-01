# Package: vault

## Overview
The `vault` package provides age-encrypted secret storage and delivery for AegisClaw skill agents. Each secret is stored as an individually encrypted `.age` file with a separate plaintext metadata index. The vault's age X25519 identity is derived deterministically from the daemon's Ed25519 signing key using HKDF-SHA256, ensuring the vault key is always recoverable without storing a separate secret. A `SecretProxy` handles resolving secrets and assembling them into injection payloads for delivery to guest VMs over vsock, with memory zeroing after transmission.

## Files
- `vault.go`: `Vault` type — `Add`, `Get`, `Delete`, `List`, `ListForSkill`, `Has`, `GetEntry`; deterministic key derivation; name validation
- `bech32.go`: Minimal BIP173 bech32 encoder for formatting the age secret key
- `proxy.go`: `SecretProxy` — `ResolveSecrets`, `BuildPayload`, `Zero`
- `vault_test.go`: Integration tests for all CRUD operations, name validation, and persistence
- `proxy_test.go`: Tests for secret resolution, skill ownership enforcement, payload building, and memory zeroing
- `security_test.go`: Security tests — path traversal prevention, size limits, deterministic derivation, ownership isolation

## Key Abstractions
- `Vault`: thread-safe encrypted secret store; per-secret `.age` files with shared metadata index
- `SecretEntry`: metadata-only record (name, skill ID, size, timestamps) — no plaintext in the index
- `SecretProxy`: delivery agent; enforces skill ownership on every secret access
- Key derivation: HKDF-SHA256 over Ed25519 private key with domain `"aegisclaw-vault-age-identity-v1"`
- `validateSecretName`: regex + path-traversal checks; max 128 characters

## System Role
The vault is used by the sandbox runtime to deliver skill secrets to guest VMs at boot time. It is also used by the `aegisclaw secrets` CLI command for operator management. All secrets are encrypted at rest; the only window where they exist in plaintext is during the vsock injection, which is immediately followed by a `Zero` call.

## Dependencies
- `filippo.io/age`: envelope encryption
- `golang.org/x/crypto/hkdf`: key derivation
- `crypto/sha256`, `crypto/ed25519`: cryptographic primitives
- `encoding/json`: metadata index persistence
- `sync`: `RWMutex` for concurrent access
