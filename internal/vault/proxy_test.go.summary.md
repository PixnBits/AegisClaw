# proxy_test.go

## Purpose
Tests for `SecretProxy` covering the secret resolution flow, skill ownership enforcement, payload building, and the memory-zeroing `Zero` function. Tests use a real vault with a temporary directory to exercise the full chain from storage to injection payload.

## Key Types and Functions
- `TestNewSecretProxy_NilPanics`: verifies the constructor panics when passed a nil vault
- `TestResolveSecrets_Success`: adds secrets to the vault, calls `ResolveSecrets` with the correct skill ID, and verifies the returned injections contain the correct plaintext
- `TestResolveSecrets_MissingSecret`: calls `ResolveSecrets` with a name not in the vault; verifies an error is returned
- `TestResolveSecrets_WrongSkill`: adds a secret for skill A, calls `ResolveSecrets` for skill B; verifies an ownership error is returned
- `TestBuildPayload`: wraps injections with `BuildPayload`; verifies the request contains all entries in order
- `TestZero`: calls `Zero` on injections and verifies all `Payload` byte slices contain only zero bytes afterward

## Role in the System
Ensures that secret delivery to guest VMs correctly enforces skill ownership boundaries and that the memory-zeroing mechanism actually clears the bytes, reducing post-delivery exposure on the host.

## Dependencies
- `testing`, `t.TempDir()`, `crypto/ed25519`
- `internal/vault`: `Vault`, `NewSecretProxy`, `SecretProxy`
