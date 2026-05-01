# globals.go — cmd/aegisclaw

## Purpose
Declares the four package-level variables that back the CLI's persistent global flags, shared across every subcommand.

## Key Types / Functions
- `globalJSON bool` — `--json`: emit structured JSON output.
- `globalVerbose bool` — `--verbose` / `-v`: increase log verbosity.
- `globalDryRun bool` — `--dry-run`: simulate without side effects.
- `globalForce bool` — `--force`: skip interactive confirmations (logged in audit trail).

## System Fit
All subcommand run functions read these variables to alter their output format or skip prompts.

## Notable Dependencies
None (no imports).
