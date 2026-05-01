# main.go — cmd/aegisclaw

## Purpose
Minimal binary entry point for the `aegisclaw` CLI. Delegates immediately to `Execute()` defined in `root.go`.

## Key Functions
- `main()` — calls `Execute()` and returns; all logic lives in subcommand files.

## System Fit
Acts as the sole `package main` entry point that the Go toolchain compiles into the `aegisclaw` binary.

## Notable Dependencies
- No imports beyond the implicit `main` package; all behaviour is in sibling files.
