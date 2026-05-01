# `scripts/build-rootfs.sh` — Summary

## Purpose

Builds minimal Alpine Linux ext4 rootfs images for AegisClaw Firecracker microVMs. Supports three target images via a `--target` flag, each serving a different system component. This is the primary rootfs provisioning script for the production VM fleet.

## Targets

| Target | Default Output | Size | Binary | Purpose |
|---|---|---|---|---|
| `guest` (default) | `…/alpine.ext4` | 256 MB | `guest-agent` | Sandbox VMs: agent, court reviewers, builder, skills |
| `aegishub` | `…/aegishub-rootfs.ext4` | 64 MB | `aegishub` | AegisHub IPC router microVM |
| `portal` | `…/portal-rootfs.ext4` | 96 MB | `aegisportal` | Web dashboard microVM |

## What It Does

1. **Builds the target binary** as a static CGO-disabled Linux/amd64 binary using the host Go toolchain (searches common paths even when invoked via `sudo`).
2. **Creates a blank ext4 image** of the target size, labelled with the target name.
3. **Mounts the image** via loop device.
4. **For `guest` target only**: installs Alpine base (`alpine-base`, `busybox`) and scripting runtimes (`python3`, `nodejs`, `bash`) — either via host `apk` or by downloading a minirootfs tarball.
5. **For `aegishub` and `portal`**: bare image — no Alpine packages, minimal attack surface.
6. **Installs the binary** at its destination path and creates an `/init` symlink (PID 1).
7. **Configures** hostname, `/etc/resolv.conf` (guest only), minimal `/etc/passwd` and `/etc/group`.
8. **Shrinks** the image with `e2fsck` + `resize2fs -M` and marks it read-only (`chmod 444`).

## Security Notes

- AegisHub rootfs is intentionally bare: no shell, no tools, minimal attack surface.
- Guest rootfs includes `/run/secrets` with `700` permissions — secrets are injected at runtime, never persisted.
- All images are marked read-only at the file level after build.

## Fit in the Broader System

Called by `make build-rootfs`. The produced images are consumed by `internal/sandbox` (Firecracker VM launcher) and `internal/provision` (automatic first-run provisioning). Requires: root privileges, `e2fsprogs`, Go toolchain.
