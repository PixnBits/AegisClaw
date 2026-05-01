# script_tools.go — cmd/aegisclaw

## Purpose
Implements the `run_script` and `list_script_languages` tools that allow the agent to execute small inline code snippets in a sandboxed environment.

## Key Constants / Types
- `maxScriptSize = 32 KiB` — hard cap on script size.
- `maxScriptArgs = 16` / `maxScriptArgLen = 256` — argument limits.
- `scriptRuntimeCommands` — map of language → `[binary, flag]` pairs for `python`, `javascript`, `bash`, `sh`.
- `runScriptParams` — `{ language, code, args, timeout_ms }`.
- `parseRunScriptParams(argsJSON)` — validates language, size, and argument constraints.
- `runScript(ctx, params)` — executes the script via `os/exec` with a timeout; returns stdout, stderr, and exit code.
- `listScriptLanguages()` — returns the sorted list of supported languages.

## System Fit
Registered in `tool_registry.go` as built-in tools available to the agent. Scripts run inside the daemon process; sandboxing relies on the host's namespaces + Firecracker isolation for the agent itself.

## Notable Dependencies
- Standard library (`os/exec`, `context`, `time`).
