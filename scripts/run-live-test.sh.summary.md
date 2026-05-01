# `scripts/run-live-test.sh` — Summary

## Purpose

End-to-end live test runner for the `TestFirstSkillTutorialLive` integration test. It resets all AegisClaw state directories, stops any running daemon processes, runs the live test with a 20-minute timeout, and collects structured log artifacts for post-run analysis.

## What It Does

1. **Prompts for confirmation** (skippable with `--yes` / `-y`) before making any destructive changes.
2. **Stops running services**: attempts `systemctl stop aegisclaw.service`, then falls back to `pkill` for `aegisclaw` and `guest-agent` processes.
3. **Removes state directories** for both the invoking user and root:
   - `~/.config/aegisclaw`
   - `~/.local/share/aegisclaw`
   - `~/.cache/aegisclaw`
4. **Runs the live test** via `sudo env PATH="$PATH" go test ./cmd/aegisclaw -run '^TestFirstSkillTutorialLive$' -timeout 20m -v -count=1`, streaming output to both console and a timestamped log file in `logs/live-test-<timestamp>/`.
5. **Collects auxiliary artifacts**: copies `aegisclaw.log` and captures the last 500 lines of the systemd journal.
6. **Exits** with the test process's exit code.

## Key Details

- Log directory: `logs/live-test-<ISO8601-timestamp>/`
- Test timeout: 20 minutes
- Requires: root access (via sudo), a running Ollama instance, Firecracker/KVM, and a built `aegisclaw` binary.
- The `PATH` is preserved across `sudo` so the user's `go` binary is found when elevated.

## Fit in the Broader System

Used by developers and CI operators to run a full end-to-end first-skill lifecycle test against real infrastructure (Ollama + Firecracker + KVM). Pairs with `testdata/cassettes/` (VCR replay mode) for non-live runs. State reset ensures test isolation across runs by clearing audit logs, proposal stores, and other accumulated daemon state.

## Notable Dependencies

- `cmd/aegisclaw` — the binary under test
- `go test` + `-tags` free (live test uses `//go:build livetest` guard)
- `journalctl` — optional, for system log capture
