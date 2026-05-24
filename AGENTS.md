# Agents

This file contains the canonical instructions for common operational tasks (especially daemon lifecycle) when working in this tree.

## Start and Stop Controls (Follow Exactly)

The Host Daemon **must** run as root on Linux (for Firecracker microVMs and privileged operations).

**Recommended (preferred):**
- `make start` — starts the daemon (internally uses `sudo ./bin/aegis start`)
- `make stop` — stops the daemon (no sudo required)

**Manual equivalents:**
- Start (with logging): `sudo ./bin/aegis start &> aegis.log`   (or `sudo ./bin/aegis start --foreground` for debugging)
- Stop: `./bin/aegis stop`

**Important notes on sudo / passwords:**
- The `sudo` commands above will prompt for a password on most machines.
- On the original development environment, passwordless sudo (NOPASSWD) was configured for these exact commands, which is why previous notes said "no password needed".
- **Do not assume passwordless sudo** when working on other machines, in CI, or when onboarding new contributors. Update your sudoers if you want passwordless operation for development:
  ```
  yourusername ALL=(ALL) NOPASSWD: /path/to/bin/aegis, /path/to/scripts/build-microvms-docker.sh
  ```
  (Adjust paths and be extremely careful with NOPASSWD rules.)

**MicroVM / rootfs builds:**
- `make build-microvms` (or direct `bash scripts/build-microvms-docker.sh`)
- The script internally uses `sudo` when it needs to create or chown directories under `/opt/aegis` (common on Linux). It will prompt unless you have configured NOPASSWD for the specific operations or run the whole build as root (not recommended).
- On non-Linux or when using Docker sandboxes, microVM builds are often skipped.

## Running Tests (After Starting the Daemon Where Required)

- Unit tests (safe, no daemon needed): `make test` or `go test ./...`
- Integration tests that exercise the running daemon: `make test-integration`
- E2E / Playwright tests (web portal): `make test-e2e` or `npm test`

Many E2E and integration tests require the full daemon + Hub + components to be running first (use `make start` per the rules above). The thin web-portal binary can be exercised in isolation via its own test fixtures for contract-level checks.

## Other Common Commands

- `make build` / `make build-binaries`
- `make doctor`
- `make status`
- `make clean`

See the Makefile for the full list of targets and the current implementation of start/stop.

## Golden Rules

- Never start or stop the daemon except via the exact mechanisms documented here.
- When in doubt while working on this branch, re-read this file before issuing any privileged or lifecycle command.
- Full user journeys (especially those involving Court, Builder, and real microVMs) can only be meaningfully tested with the daemon actually running.
