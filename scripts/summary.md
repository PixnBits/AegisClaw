# `scripts/` — Directory Summary

## Overview

The `scripts/` directory contains shell and Python utility scripts for building Firecracker microVM root filesystem images, generating Go source files, and running end-to-end integration tests. These scripts are developer and operator tools — they are not part of the compiled Go binary or the normal test suite.

## Files

| File | Language | Purpose |
|---|---|---|
| `build-rootfs.sh` | Bash | Build Alpine ext4 rootfs images for guest-agent, AegisHub, and portal microVMs |
| `build-builder-rootfs.sh` | Bash | Build the 2 GB builder rootfs with Go toolchain + lint/security tools |
| `gen_sandbox.py` | Python 3 | Generate `internal/sandbox/spec.go` and `manager.go` from Python string templates |
| `run-live-test.sh` | Bash | Reset daemon state and run `TestFirstSkillTutorialLive` with artifact collection |

## Usage Patterns

### Rootfs Image Build (one-time setup)
```bash
make build-rootfs                           # all three production images
sudo ./scripts/build-rootfs.sh              # guest-agent image only
sudo ./scripts/build-rootfs.sh --target=aegishub
sudo ./scripts/build-rootfs.sh --target=portal
sudo ./scripts/build-builder-rootfs.sh      # builder image
```

### Code Generation
```bash
python3 scripts/gen_sandbox.py              # regenerate internal/sandbox/{spec,manager}.go
```

### Live End-to-End Test
```bash
./scripts/run-live-test.sh                  # with confirmation prompt
./scripts/run-live-test.sh --yes            # non-interactive
```

## Fit in the Broader System

- `build-rootfs.sh` and `build-builder-rootfs.sh` produce the disk images consumed by `internal/sandbox`, `internal/provision`, and `internal/builder` at daemon startup and during skill builds.
- `gen_sandbox.py` produces the foundational Go types used throughout `internal/sandbox` and all packages that manage VM lifecycle.
- `run-live-test.sh` automates the full live integration test workflow documented in `CONTRIBUTING.md`.

## Prerequisites

- `build-rootfs.sh` / `build-builder-rootfs.sh`: root privileges, `e2fsprogs`, Docker (builder script), Go toolchain.
- `run-live-test.sh`: root privileges, Ollama, Firecracker + jailer, `/dev/kvm`.
