# default_bootstrap.go — cmd/aegisclaw

## Purpose
Ensures the `default-script-runner` skill VM is started and registered before the daemon accepts any chat requests.

## Key Types / Functions
- `ensureDefaultScriptRunnerActive(ctx, env)` — checks if the `default-script-runner` skill is registered in the skill registry; if not present or not running, activates it via the normal skill activation path.

## System Fit
Called during `runStart` after the API server is up. Guarantees baseline tool availability (script execution, workspace file operations) without requiring operator intervention.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/skill` — registry lookup and VM activation
