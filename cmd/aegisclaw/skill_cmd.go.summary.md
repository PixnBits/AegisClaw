# skill_cmd.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw skill` subcommand tree: `add`, `list`, `revoke`, `info`, `sbom`, and `activate`. Manages the lifecycle of skills (isolated microservice VMs) including interactive wizard, SBOM inspection, and pre-activation secret checking.

## Key Types / Functions
- `runSkillAdd(cmd, args)` — interactive wizard (`wizard.WizardResult`) or `--non-interactive` mode from flags. Creates skill definition and optionally activates it.
- `runSkillList(cmd, args)` — queries daemon first (live list), falls back to local skill registry.
- `runSkillRevoke(cmd, args)` — marks a skill inactive in the registry and stops its VM.
- `runSkillInfo(cmd, args)` — prints metadata, SBOM summary, and current status.
- `runSkillSBOM(cmd, args)` — prints the full SBOM for a skill.
- `runSkillActivate(cmd, args)` — pre-activation secret check; then routes to daemon `skill.activate`.

## System Fit
Primary lifecycle management for skills. The `activate` path enforces that all required secrets are provisioned before the skill VM is started.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/skill` — skill registry
- `github.com/PixnBits/AegisClaw/internal/wizard` — interactive wizard
- `github.com/PixnBits/AegisClaw/internal/sbom` — SBOM reading
