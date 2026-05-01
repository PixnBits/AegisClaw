# eval_cmd.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw eval` subcommand tree: `run` and `report`. Runs evaluation scenarios against the live agent to measure capability and regression.

## Key Types / Functions
- `runEvalRun(cmd, args)` — initialises `eval.Runner`, optionally filters by `--scenario` name, runs all matching scenarios, prints pass/fail per scenario.
- `runEvalReport(cmd, args)` — reads stored evaluation results and prints a summary table.
- `cliEvalProbe` — implements `eval.DaemonProbe` by routing eval tool calls through the daemon API.

## System Fit
Quality gate for agent behaviour. Scenarios are defined in `testdata/evals/`. Uses the real running daemon so results reflect actual model + skill state.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/eval` — `Runner`, `DaemonProbe`
- `github.com/PixnBits/AegisClaw/internal/api` — daemon API client
