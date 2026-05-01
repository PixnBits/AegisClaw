# provision.go

## Purpose
Downloads and assembles the low-level kernel and root filesystem assets required to launch Firecracker microVMs. It fetches a pre-built `vmlinux` kernel image from a CI S3 bucket and constructs a minimal Alpine 3.21 ext4 root filesystem suitable for the AegisClaw guest agent. The provisioning process installs the `guest-agent` binary as `/sbin/guest-agent` with a symlink from `/init`, and creates the `/workspace` and `/run/secrets` directories. After building, the image is shrunk with `e2fsck` and `resize2fs`.

## Key Types and Functions
- `AssetConfig`: configuration struct with `KernelPath`, `RootfsPath`, and architecture fields
- `EnsureAssets(ctx context.Context, cfg AssetConfig, logger) error`: idempotent entry point; downloads kernel if absent, builds rootfs if absent
- Arch detection: maps `amd64` → `x86_64`, `arm64` → `aarch64` for download URL construction
- `maxDownloadBytes = 512 MiB`: hard cap on kernel download size to prevent resource exhaustion
- Requires root privileges; depends on system tools: `dd`, `mkfs.ext4`, `mount`, `umount`, `e2fsck`, `resize2fs`
- Alpine minirootfs tarball downloaded from the official Alpine CDN

## Role in the System
Provision is called during AegisClaw daemon startup or during the `aegisclaw provision` CLI command to ensure that sandbox VM assets are present and valid before any sandboxes are launched. Without these assets, `FirecrackerRuntime` cannot create microVMs.

## Dependencies
- `net/http`: kernel and rootfs tarball downloads
- `os/exec`: invokes `dd`, `mkfs.ext4`, `mount`, `umount`, `e2fsck`, `resize2fs`
- `io`: `LimitReader` for download size cap
- Standard library: `archive/tar`, `compress/gzip`, `context`, `os`, `path/filepath`
