# `loader.go` — Persona YAML Loader

## Purpose
Reads and validates `Persona` definitions from a directory of YAML files. Also provides functions to locate the default persona directory and seed it with built-in default personas when it does not yet exist.

## Key Functions

| Function | Description |
|---|---|
| `LoadPersonas(dir, logger)` | Reads all `*.yaml`/`*.yml` files in `dir`, unmarshals each as a `Persona`, validates, and returns the slice |
| `DefaultPersonaDir()` | Returns `~/.config/aegisclaw/personas` |
| `EnsureDefaultPersonas(logger)` | Creates the persona directory and writes built-in YAML files if the directory does not exist |

## Logic Notes
- Non-YAML files and subdirectories are silently skipped.
- If no persona files are found the function returns an error (engine requires ≥1 persona).
- Each loaded persona is validated via `Persona.Validate()` before being added to the result.

## Role in the System
Called at daemon startup (and by court integration tests) to populate the `[]*Persona` slice passed to `NewEngine`. Keeps persona definitions as data (YAML), not code, allowing operators to customize court reviewer behavior without recompiling.

## Notable Dependencies
- `go.uber.org/zap`
- `gopkg.in/yaml.v3`
