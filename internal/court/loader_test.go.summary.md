# `loader_test.go` — Tests for the Persona Loader

## Purpose
Verifies that `LoadPersonas` correctly reads valid YAML persona files, rejects invalid ones, and handles edge cases such as empty directories and non-YAML files.

## Key Test Cases

| Test | What It Verifies |
|---|---|
| `TestLoadPersonas_Valid` | A directory with one well-formed YAML file returns one valid `*Persona` |
| `TestLoadPersonas_EmptyDir` | Returns error when no YAML files are present |
| `TestLoadPersonas_InvalidYAML` | Returns error on malformed YAML |
| `TestLoadPersonas_FailsValidation` | Returns error when persona fails `Validate()` (e.g., missing name, weight out of range) |
| `TestLoadPersonas_SkipsNonYAML` | `.txt` and subdirectory entries are ignored |
| `TestEnsureDefaultPersonas` | Creates persona directory and writes default files when the directory is absent |

## Role in the System
Ensures the loader is robust against filesystem edge cases so the daemon can fail fast with a clear error on misconfigured persona directories rather than silently launching with no reviewers.

## Notable Dependencies
- Package under test: `court`
- Standard library (`os`, `path/filepath`, `testing`)
