# secrets_injection_test.go — cmd/aegisclaw

## Purpose
Tests that secrets are correctly injected into the skill VM environment by the `vault.secret.add` and `vault.secret.refresh` handlers.

## Key Helpers / Tests
- `makeTestVault(t)` — creates a temporary vault with a fresh Ed25519 key.
- Tests verify: secrets are encrypted on write, plaintext is never stored, refresh correctly updates the VM-visible value, and `rotate` rejects missing secrets.

## System Fit
Prevents regressions in the secret-injection security contract. Uses the real `vault.Vault` implementation with temp dirs; no KVM required.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/vault`
- `github.com/PixnBits/AegisClaw/internal/proposal`
- `github.com/PixnBits/AegisClaw/internal/sandbox`
