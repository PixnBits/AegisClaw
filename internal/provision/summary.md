# Package: provision

## Overview
The `provision` package handles the download and assembly of the kernel image and root filesystem required to run Firecracker microVMs. It is responsible for fetching a pre-built `vmlinux` from CI artifact storage, constructing a minimal Alpine Linux ext4 root filesystem, and installing the AegisClaw `guest-agent` binary into it. The package is designed to be idempotent: if the assets already exist on disk they are not re-downloaded or rebuilt.

## Files
- `provision.go`: `EnsureAssets` entry point, kernel download, Alpine rootfs build, guest-agent installation, and image shrink logic

## Key Abstractions
- `AssetConfig`: describes where to write the kernel and rootfs images, plus architecture selection
- `EnsureAssets`: the single public entry point; safe to call on every daemon start
- Architecture mapping: `amd64`/`arm64` host arch is translated to the correct Alpine and Firecracker asset URL paths
- Download safety: `maxDownloadBytes = 512 MiB` hard cap via `io.LimitReader`

## System Role
Provision is a prerequisite for the sandbox subsystem (`internal/sandbox`). The `FirecrackerRuntime.NewFirecrackerRuntime` expects valid kernel and rootfs paths. The `aegisclaw provision` CLI subcommand and the daemon startup sequence call `EnsureAssets` to satisfy this requirement before attempting to create any microVM. Without this package, no sandboxed skill execution is possible.

## Dependencies
- `net/http`: downloads kernel and Alpine minirootfs tarballs
- `os/exec`: invokes low-level Linux filesystem tools (`dd`, `mkfs.ext4`, `mount`, `umount`, `e2fsck`, `resize2fs`)
- `archive/tar`, `compress/gzip`: tarball extraction
- `io`: bounded reading
- Standard library: `os`, `path/filepath`, `context`, `runtime`
