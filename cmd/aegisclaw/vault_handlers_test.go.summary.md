# vault_handlers_test.go — cmd/aegisclaw

## Purpose
Unit tests for the vault API handlers (`vault.secret.add`, `vault.secret.list`, `vault.secret.rotate`, `vault.secret.refresh`) using a real vault and kernel backed by temp directories.

## Key Helpers / Tests
- `testEnvWithVaultAndKernel(t)` — creates a `runtimeEnv` with a real vault and kernel; resets the kernel singleton on cleanup.
- Tests cover: add a secret, list returns name (not value), rotate updates value, refresh with nil vault returns error, missing skill ID returns error.

## System Fit
Validates the vault handler security contract. No KVM required.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/api`
- `github.com/PixnBits/AegisClaw/internal/kernel`
