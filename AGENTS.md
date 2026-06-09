# Agents

This file contains the canonical instructions for common operational tasks (especially daemon lifecycle) when working in this tree.

## Start and Stop Controls (Follow Exactly)

The Host Daemon **must** run as root on Linux (for Firecracker microVMs and privileged operations).

**Recommended (preferred):**
- Start: `sudo ./bin/aegis start`
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

## Agent Behavior for Sudo and Privileged Commands

When performing tasks that require privileged operations (daemon lifecycle, microVM builds, rootfs preparation, E2E tests that depend on a running daemon, etc.):

- **Always attempt the sudo commands first.** The environment *may* already have the exact commands allowed via passwordless (NOPASSWD) rules matching `scripts/aegisclaw-sudoers.example`. Use the documented mechanisms:
  - Prefer direct CLI: `sudo -n ./bin/aegis start`, `sudo -n ./bin/aegis start --foreground`, `bash scripts/build-microvms-docker.sh` (or `make build-microvms`), `make test-e2e*`, etc.
  - For direct control use `sudo -n ./bin/aegis start ...` (the `-n` is important for non-interactive attempts).
  - Scripts (and the Makefile for builds/tests) use `sudo -n` where appropriate for certain build/clean steps. For daemon start/stop, always use the direct `sudo -n ./bin/aegis ...` form.

- **Report every attempt precisely.** If a sudo command fails or is rejected, immediately surface:
  - The **exact full command** that was executed (including all flags, paths, and environment variables).
  - The **complete error/rejection output** (stdout + stderr) — e.g. "sudo: a password is required", "interactive authentication required", "Sorry, user ... may not run sudo ...", permission denied on the binary, etc.

- **On sudo authentication or permission failure (password required, etc.):**
  - **Do not** immediately give up or skip the operation (especially E2E/daemon-dependent tests).
  - Proactively update `scripts/aegisclaw-sudoers.example` to add the exact failing command (following the existing pattern for `bin/aegis` and scripts like `build-microvms-docker.sh`, `create-firecracker-rootfs.sh`, etc.).
  - Provide the user with clear, copy-paste instructions to install the updated rules:
    ```
    # Edit YOURUSER and full absolute paths in scripts/aegisclaw-sudoers.example first
    sudo cp scripts/aegisclaw-sudoers.example /etc/sudoers.d/aegisclaw
    sudo chmod 440 /etc/sudoers.d/aegisclaw
    sudo visudo -c   # validate; must succeed with no errors
    ```
  - Also consider adding `Defaults` lines for env_keep (e.g. `AEGIS_BOOT_TIMING`, `AEGIS_DEFAULT_MODEL`, `AEGIS_HUB_SOCKET`, etc.) if the command needs them under sudo.
  - After the user applies the rules (or if you can apply them in the current context), re-attempt the exact command.

- **Prioritize running the daemon for E2E tests.** Explicitly run the aegis start command (via `sudo -n ./bin/aegis start --foreground` or `sudo ./bin/aegis start --foreground` etc.) when E2E, smoke, integration, or collaboration-model tests are needed. Do **not** skip `make test-e2e`, `make test-e2e-llm`, `make smoke`, `scripts/verify-pm-llm-e2e.sh`, or similar just because sudo is involved. Full user journeys (especially channels, Project Manager + real LLM, Court, on-demand roles, boot metrics) require the actual daemon + microVMs + Hub to be running. Persist through sudo setup if necessary to enable these tests. Use the E2E scripts' built-in support for existing daemons or isolated runs where possible, but get a real daemon up.

- When in doubt, re-read this section and the Start/Stop Controls before deciding to skip privileged steps. The goal is to exercise the real system.

**Environment variables under sudo:**
- The daemon no longer requires you to manually export `AEGIS_ROOTFS_DIR` and `AEGIS_KERNEL_PATH` through sudo in most cases.
- When started via `sudo`, it automatically detects the original user via `SUDO_USER` and prefers that user's `~/.aegis/firecracker/` directory for kernels and images.
- A wrapper script is now *optional* (only needed if you want to force specific paths or enable debug logging). Simple `sudo ./bin/aegis start` should "just work" after you run `make build-microvms` as your normal user.

**MicroVM / rootfs builds:**
- `bash scripts/build-microvms-docker.sh` (or `make build-microvms`)
- The script internally uses `sudo` when it needs to create or chown directories under `/opt/aegis` (common on Linux). It will prompt unless you have configured NOPASSWD for the specific operations or run the whole build as root (not recommended).
- On non-Linux or when using Docker sandboxes, microVM builds are often skipped.

## Accessing the Web UI for Review (SSH / Remote Machines)

The Web Portal is only reachable through the Host Daemon's hardened reverse proxy (see `web-portal-vm.md`).

**Default (localhost only):**
- UI available at `http://localhost:8080` on the machine running the daemon.

**From another computer (SSH session):**

**Recommended safe method — SSH local port forwarding (no changes to binding):**
```bash
# On your local machine
ssh -L 8080:localhost:8080 user@remote-server
# Then open http://localhost:8080 in your local browser
```

**Alternative (bind to all interfaces — use only on trusted networks):**
```bash
AEGIS_WEB_PORTAL_PROXY_ADDR=0.0.0.0:8080 sudo ./bin/aegis start
```
- You will see a warning in the logs when binding non-localhost.
- Then access `http://<server-ip>:8080` from your other computer.
- **Security note**: This exposes the rich UI (and any unauthenticated actions) to the network. Only use for short review/debug sessions.

The internal portal address can also be overridden with `AEGIS_WEB_PORTAL_INTERNAL_ADDR` if needed.

After review, stop the daemon normally (`./bin/aegis stop` or Ctrl+C in foreground mode) and restart with default localhost binding for normal use.

## Running Tests (After Starting the Daemon Where Required)

See `docs/testing-standards.md` for the authoritative testing strategy, recommended layers, and LLM-specific guidance.

Quick reference:
- Unit tests (safe, no daemon needed): `make test` or `go test ./...`
- Integration tests that exercise the running daemon: `make test-integration`
- E2E / Playwright tests (web portal): `make test-e2e` or `npm test`
- Collaboration / LLM E2E (real unmocked, requires daemon + Ollama): `make test-e2e-llm` or `bash scripts/verify-pm-llm-e2e.sh`
- Smoke test after start: `make smoke`

**Critical for LLM Agents:**
Many E2E and integration tests (including `test-e2e`, `test-e2e-llm`, smoke, and collaboration features) **require the full daemon + Hub + microVMs + Court + base infrastructure to be running**. Use `sudo ./bin/aegis start` (or `sudo ./bin/aegis start --foreground` for debugging) per the Start/Stop Controls. 

**Do not skip E2E tests** simply because they involve sudo or starting the daemon. The purpose of `test-e2e-llm`, the verify script, smoke after start, etc. is to validate the real user experience (including channels, Project Manager driving real LLM plans into channels, role ensures, boot metrics, etc.). Follow the sudo setup process described earlier to get `sudo -n` working for the required commands rather than falling back to contract-only fixtures or unit tests.

## LLM Agent Guidance When Working on This Project

This is a microVM + Firecracker project. Most LLMs have limited training data on vsock, custom rootfs images, pre-warm pools, component registration via AegisHub, and the specific startup sequencing used here. Therefore:

- Treat `docs/testing-standards.md` as required reading before making changes that affect daemon startup, pre-warm logic, channels, or the Project Manager.
- Always run `make smoke` (or the equivalent CLI-driven checks) and inspect `aegis status` + `aegis vm pools` early when working on startup, lifecycle, or collaboration features.
- Prefer `make test-e2e-contract` for rapid iteration on portal/API surfaces.
- Use `make test-e2e-llm` as the primary verification for collaboration model work. It should demonstrate real LLM output appearing in channels.
- Explicitly assert on startup health invariants in tests you create or modify (see `docs/testing-standards.md` for the list).
- When you discover or fix a bug related to startup, pre-warm, or component registration, add or improve an automated test that would have caught it.
- Update `docs/testing-standards.md` and this file when the definition of "healthy system" changes.

## Other Common Commands

- `make build` / `make build-binaries`
- `./bin/aegis doctor`
- `./bin/aegis status`
- `make sbom` (7.8 SBOM + cosign hooks; additive, see Makefile)
- `make clean`

See the Makefile for build, test, smoke, and other convenience targets. Prefer using the `aegis` CLI directly when possible.

## Golden Rules

- Never start or stop the daemon except via the exact mechanisms documented here.
- When in doubt while working on this branch, re-read this file and `docs/testing-standards.md` before issuing any privileged or lifecycle command.
- Full user journeys (especially those involving Court, Builder, channels, and real microVMs) can only be meaningfully tested with the daemon actually running.
- For any privileged operation (especially `sudo ./bin/aegis start` / `./bin/aegis start` to unblock E2E tests), follow the "Agent Behavior for Sudo and Privileged Commands" section: attempt with `sudo -n`, report exact command + full rejection output, proactively extend `scripts/aegisclaw-sudoers.example` and give the user installation instructions, then re-try. Do not skip E2E or daemon-dependent work due to sudo friction.
