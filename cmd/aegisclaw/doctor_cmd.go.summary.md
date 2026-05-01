# doctor_cmd.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw doctor` command — a standalone health-check tool that does not require a running daemon. Verifies the local installation is complete and configured correctly.

## Key Types / Functions
- `runDoctor(cmd, args)` — runs a checklist: required binaries (Firecracker, jailer), required files (kernel image, rootfs), required directories, Ollama reachability, daemon socket accessibility, isolation mode. Prints a ✓/✗ table.

## System Fit
First-resort debugging tool. Because it requires no daemon, it can diagnose startup failures. Different from `self diagnose` which runs extended checks from inside the running daemon context.

## Notable Dependencies
- `net/http` — Ollama probe
- Standard library only (no internal packages).
