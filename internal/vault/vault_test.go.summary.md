# vault_test.go

## Purpose
Integration tests for the `Vault` type covering all CRUD operations and secret name validation. Tests use a real age X25519 identity derived from a generated Ed25519 key to exercise the full encryption/decryption path. Each test uses a temporary directory to ensure isolation.

## Key Types and Functions
- `TestNewVault`: verifies vault creation and index initialisation
- `TestAdd_Get`: adds a secret and verifies `Get` returns the exact bytes after decryption
- `TestAdd_Overwrite`: adds a secret twice and verifies the latest value is returned
- `TestDelete`: adds then deletes a secret; verifies `Get` returns an error and `Has` returns false
- `TestList`: adds multiple secrets and verifies all appear in `List` with correct metadata
- `TestListForSkill`: adds secrets for two different skills; verifies `ListForSkill` returns only the matching skill's entries
- `TestHas`: verifies `Has` returns true for present and false for absent secrets
- `TestGetEntry`: verifies metadata is correct without decrypting the secret value
- `TestValidateSecretName_Valid`: verifies valid names (alphanumeric, underscores, hyphens) pass validation
- `TestValidateSecretName_Invalid`: verifies names starting with digits, containing path separators, or exceeding 128 characters are rejected
- `TestPersistence`: creates a vault, adds a secret, creates a new vault from the same directory, and verifies the secret is still readable

## Role in the System
Ensures the vault's encryption, metadata management, and name validation are correct and that secrets survive daemon restarts.

## Dependencies
- `testing`, `t.TempDir()`, `crypto/ed25519`: key generation for test identity
- `internal/vault`: package under test
