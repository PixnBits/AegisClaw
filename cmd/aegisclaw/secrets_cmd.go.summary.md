# secrets_cmd.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw secrets` subcommand tree: `add`, `list`, `rotate`, and `refresh`. Provides secure terminal prompting for secret values and routes operations through the vault.

## Key Types / Functions
- `runSecretsAdd(cmd, args)` — disables terminal echo via `unix.IoctlGetTermios`, reads value, calls daemon `vault.secret.add`.
- `runSecretsList(cmd, args)` — lists secret names (not values) for a skill scope.
- `runSecretsRotate(cmd, args)` — prompts for new value; calls daemon `vault.secret.rotate`.
- `runSecretsRefresh(cmd, args)` — calls daemon `vault.secret.refresh` which re-injects the current secret value into the running skill VM's `/run/secrets` tmpfs without restarting it.

## System Fit
Sensitive operation — secret values never appear in command history because terminal echo is disabled. The `refresh` subcommand enables zero-downtime secret rotation.

## Notable Dependencies
- `golang.org/x/sys/unix` — `IoctlGetTermios` / `IoctlSetTermios` for echo suppression
- `github.com/PixnBits/AegisClaw/internal/api` — daemon API client
