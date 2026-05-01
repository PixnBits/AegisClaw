# `scripts/build-builder-rootfs.sh` — Summary

## Purpose

Builds the **builder rootfs template** — a 2 GB Alpine Linux ext4 filesystem image pre-loaded with the complete Go toolchain, `golangci-lint`, `staticcheck`, and `gosec`. This image is used as the read-only root filesystem for builder sandbox Firecracker microVMs that compile and vet skill code.

## What It Does

1. Creates a 2048 MB ext4 image at the output path (default: `/var/lib/aegisclaw/rootfs-templates/builder.ext4`).
2. Mounts the image and uses the official Alpine Docker image (`alpine:3.21`) to install Alpine base packages (`alpine-base`, `bash`, `openssl`, `ca-certificates`, `curl`, `wget`, `git`, `make`, `gcc`, `musl-dev`, `linux-headers`) with signature verification.
3. Downloads and installs Go `1.24.4` (architecture-aware: `amd64` / `arm64`) from `go.dev/dl`.
4. Inside a `chroot`, installs Go developer tools: `golangci-lint`, `staticcheck`, `gosec`. Cleans the module cache after install to reduce image size.
5. Configures `PATH`, `GOPATH`, `GOCACHE`, `GOMODCACHE` via `/etc/profile.d/go.sh`.
6. Adds a placeholder `builder-agent` init script and an fstab entry that mounts `/workspace` as a writable ext4 drive (the only writable location in the builder VM).

## Security Notes

- Alpine package signatures are verified using Alpine's own trusted keys (inside the container) — no `--allow-untrusted` flag.
- The resulting rootfs is **read-only** at runtime; only `/workspace` is writable via a separate drive.
- Runs on the host during one-time setup only.

## Key Variables

| Variable | Default | Description |
|---|---|---|
| `OUTPUT` | `/var/lib/aegisclaw/rootfs-templates/builder.ext4` | Output image path |
| `ROOTFS_SIZE_MB` | `2048` | Image size in MB |
| `GO_VERSION` | `1.24.4` | Go toolchain version |
| `ALPINE_VERSION` | `3.21` | Alpine Linux version |

## Fit in the Broader System

This image is provisioned once and consumed by `internal/builder` when launching the sandboxed builder Firecracker microVM for each skill build. Prerequisites: root access, Docker, `e2fsprogs`.
