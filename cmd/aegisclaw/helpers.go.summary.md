# helpers.go — cmd/aegisclaw

## Purpose
Shared formatting utilities used across multiple command files.

## Key Types / Functions
- `truncateStr(s, n)` — returns the first `n` runes of `s` with `"…"` appended if truncated.
- `boolYesNo(b)` — returns `"yes"` or `"no"` for human-readable flag display.

## System Fit
Reduces duplication in output formatting across audit, status, and skill subcommands.

## Notable Dependencies
- Standard library only.
