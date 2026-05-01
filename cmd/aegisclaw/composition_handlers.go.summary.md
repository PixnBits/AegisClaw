# composition_handlers.go — cmd/aegisclaw

## Purpose
Implements daemon API handlers for the composition manifest lifecycle (D10 SDLC): `composition.current`, `composition.rollback`.

## Key Types / Functions
- `makeCompositionCurrentHandler(env)` — returns the current composition manifest; returns `{"version":0}` if none is set.
- `compositionRollbackRequest` — `{ target_version int, reason string }`.
- `makeCompositionRollbackHandler(env)` — rolls back to a previous manifest version; rolls back one step if `target_version` is 0. Records a kernel-signed audit entry for the rollback reason.

## System Fit
Part of the Governance Court SDLC. Composition manifests describe the complete set of approved skills and their versions; rollback is the recovery path after a bad deployment.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/composition`
- `github.com/PixnBits/AegisClaw/internal/kernel`
