# self_cmd.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw self` subcommand tree: `propose`, `status`, and `diagnose`. Provides self-modification proposal management and environment diagnostics.

## Key Types / Functions
- `runSelfPropose(cmd, args)` — creates a self-modification proposal in the governance court.
- `runSelfStatus(cmd, args)` — shows pending self-modification proposals and their review state.
- `runSelfDiagnose(cmd, args)` — offline health checks: KVM device, Firecracker binary, rootfs images, Ollama reachability, default model availability. `--extended` flag adds RAM, disk, and snapshot-support checks.
- `probeOllama()` — issues a GET to `/api/tags` on the configured Ollama endpoint to verify it is reachable and lists the required model.

## System Fit
`diagnose` runs without a daemon and is often the first step in debugging installation issues. `propose`/`status` integrate with the Governance Court SDLC.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/api` — daemon API client (propose/status)
- `net/http` — probeOllama
