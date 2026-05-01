# court_init.go — cmd/aegisclaw

## Purpose
Initialises the Governance Court engine used for reviewing self-modification proposals and composition manifests.

## Key Types / Functions
- `initCourtEngine(env)` — loads judge personas from the config directory, creates a `FirecrackerLauncher` for spinning up review VMs, and constructs a `court.Engine` with the loaded personas.
- Returns the engine which is stored on `runtimeEnv` for use by proposal and composition handlers.

## System Fit
Called once during daemon startup in `runStart`. The court engine is a prerequisite for any SDLC flow (skill add, self propose, composition apply).

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/court` — `Engine`
- `github.com/PixnBits/AegisClaw/internal/sandbox` — `FirecrackerLauncher`
