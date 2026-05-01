# security_test.go

## Purpose
Security-focused tests for the vault package that verify path traversal prevention, secret size limits, deterministic key derivation from Ed25519 keys, and skill ownership isolation. These tests specifically target attack vectors that could allow an adversary to read secrets belonging to another skill or to exhaust host memory via oversized secret values.

## Key Types and Functions
- `TestPathTraversal_SecretName`: attempts to add secrets with names containing `../`, `/`, `\`, and null bytes; verifies all are rejected by `validateSecretName`
- `TestSecretSizeLimit`: attempts to add a secret larger than 1 MiB; verifies the operation is rejected with a size error
- `TestDeterministicKeyDerivation`: derives an age identity from the same Ed25519 key twice; verifies the resulting `age1…` bech32 strings are identical
- `TestKeyDerivation_DifferentKeys`: derives identities from two different Ed25519 keys; verifies the results differ
- `TestSkillOwnershipIsolation`: adds a secret for skill A; attempts to `Get` it as skill B; verifies access is denied
- `TestDeleteOwnership`: adds a secret for skill A; attempts to `Delete` it as skill B; verifies the operation is rejected

## Role in the System
Directly validates the security properties that the vault depends on. Regressions here could allow cross-skill secret theft or host resource abuse through oversized payloads.

## Dependencies
- `testing`, `t.TempDir()`, `crypto/ed25519`
- `internal/vault`: `Vault`, `NewVault`, `validateSecretName`
