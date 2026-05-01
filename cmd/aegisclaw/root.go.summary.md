# root.go — cmd/aegisclaw

## Purpose
Defines the Cobra command tree for the `aegisclaw` CLI. Declares the root command, top-level subcommands (`start`, `status`, `version`, `skill`, `audit`), their long-form help text, and registers all global flags and subcommand hierarchies in `init()`.

## Key Types / Functions
- `rootCmd` — root `cobra.Command`; SilenceErrors enabled, sets the public CLI surface.
- `startCmd` — `aegisclaw start`; delegates to `runStart` in `start.go`.
- `statusCmd` — `aegisclaw status`; delegates to `runStatus` in `status.go`.
- `versionCmd` — `aegisclaw version`; delegates to `runVersion` in `version.go`.
- `skillCmd` + `skillListCmd`, `skillRevokeCmd`, `skillInfoCmd` — `aegisclaw skill *` subtree.
- `auditCmd` + `auditVerifyCmd` — `aegisclaw audit *` subtree.
- `Execute()` — called from `main()`; runs the root command and calls `cobra.CheckErr`.
- `init()` — wires global flags (`--json`, `--verbose`, `--dry-run`, `--force`) and all subcommand trees (`init`, `start`, `stop`, `status`, `chat`, `skill`, `audit`, `secrets`, `self`, `version`, `memory`, `event`, `worker`, `eval`).

## System Fit
Central wiring file — every CLI subcommand is registered here. Changing the public command surface starts here.

## Notable Dependencies
- `github.com/spf13/cobra` — CLI framework
