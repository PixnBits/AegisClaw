# vault_handlers.go — cmd/aegisclaw

## Purpose
Implements daemon API handlers for vault (secret) management: `vault.secret.add`, `vault.secret.list`, `vault.secret.rotate`, and `vault.secret.refresh`.

## Key Types / Functions
- `makeVaultSecretAddHandler(env)` — stores a plaintext secret from the CLI into the vault (encrypted on disk). When `req.Rotate = true`, enforces that the secret must already exist.
- `makeVaultSecretListHandler(env)` — returns secret names and metadata (never values) for a skill scope.
- `makeVaultSecretRotateHandler(env)` — alias for add with `Rotate = true`; called by `secrets rotate`.
- `makeVaultSecretRefreshHandler(env)` — re-injects the current secret into the running skill VM's `/run/secrets` tmpfs without a VM restart.
- `vaultTimeFormat` — display timestamp format `"2006-01-02 15:04"`.

## System Fit
The daemon owns all vault write operations. The CLI sends plaintext over the local Unix socket; the daemon encrypts before writing. No secret ever transits the network.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/api` — `VaultSecretAddRequest`
- `github.com/PixnBits/AegisClaw/internal/kernel`
