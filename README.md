# AegisClaw v2

**Status:** Phase 0 - Foundations & Testing Infrastructure

AegisClaw is a secure, sandboxed AI agent runtime built for safety and reliability. All components run in isolated microVMs (Linux/Firecracker) or Docker Sandboxes (macOS/Windows).

## Architecture

- **Linux**: Firecracker microVMs for maximum isolation
- **macOS/Windows**: Docker Sandboxes for lightweight isolation
- **Host Daemon**: Minimal, privileged-only bootstrap component
- **AegisHub**: Central router enforcing strict ACLs between sandboxes

## System Requirements

### Linux (Firecracker)
- Linux 5.10+ with KVM support (`/dev/kvm` must be readable by your user or the daemon)
- Firecracker binary (see installation instructions below)
- A **minimal Firecracker-compatible kernel** (vmlinux) — **do not use a full distro kernel** like `/boot/vmlinuz-*` (this is the #1 cause of "connection refused" on the API socket)
- **Firecracker version sensitivity**: The JSON machine configuration schema and vsock device format have changed across releases. We have successfully used both v1.15.x static builds and recent `main` (v1.16.0-dev) builds. Newer builds removed `ht_enabled` (use `"smt": false` instead) and require an `uds_path` inside the vsock object. See the troubleshooting section below for the exact errors and fixes.
- Go 1.22+
- Docker (for building microVM filesystems)
- Proper environment variables for kernel and rootfs (see below)

### macOS/Windows (Docker Sandbox)
- Docker Desktop (or equivalent)
- Go 1.22+
- `sudo` privileges if using Firecracker on Linux

## Quick Start

### 1. Install Dependencies

**Linux (Firecracker)** — Firecracker is **not** reliably available via `apt`. Use the official static binary (see detailed instructions in the "Installing Firecracker on Linux" section below).

```bash
# Required on most Linux systems
sudo apt-get install -y docker.io
```

**macOS:**

```bash
brew install docker go
```

**Windows:**

Install Docker Desktop from https://www.docker.com/products/docker-desktop

> **Important for Linux users:** After installing the Firecracker binary, make sure `which firecracker` works. The daemon will now automatically look in your own `~/.aegis/firecracker/` directory (even when started via `sudo`) for the kernel and images. The old mandatory wrapper that only existed to pass two environment variables is no longer required for most people.

### 2. Build the System

```bash
# Build all binaries and microVM filesystems
make build

# Or build components separately:
make build-binaries           # Go binaries only
make build-microvms           # MicroVM filesystems (Linux only)
```

### 3. Start the Daemon

```bash
# Start in background (uses sudo, no password needed)
make start

# Or start in foreground (useful for debugging)
make start-foreground

# Stop the daemon
make stop

# Check status
make status

# Run health checks
make doctor
```

### 4. View Logs

```bash
# Daemon logs
tail -f ~/.aegis/daemon.log

# Follow all system logs
./bin/aegis logs
```

## Development

### Building Individual Components

```bash
# Build just the daemon
go build -o bin/aegis ./cmd/aegis

# Build just the web portal
go build -o bin/web-portal ./cmd/web-portal

# Build with optimizations
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/aegis ./cmd/aegis
```

### Running Tests

See [TESTING.md](./TESTING.md) for the full guide covering:

- Unit tests (`make test`)
- Integration tests
- E2E / browser tests with Playwright (isolated contract mode vs live full-system journeys)
- The E2E fixture client for reliable thin-portal testing
- Smoke tests
- Visual regression guidance (with Git LFS requirements)
- Writing and maintaining tests

Quick commands are also shown in the Makefile help (`make help`).

### Platform-Specific Notes

#### Linux (Firecracker)

**Filesystem Locations:**
- Default location: `/opt/aegis/firecracker/rootfs` (if writable)
- Fallback location: `~/.aegis/firecracker/rootfs` (user home)
- Override with env var: `AEGIS_ROOTFS_DIR=/custom/path`

The build script automatically:
1. Checks if `/opt/aegis` is writable
2. Falls back to `~/.aegis/firecracker/rootfs` if not
3. Requests `sudo` only if needed to create system directories
4. Adjusts permissions so the current user can access filesystems

**Filesystem Permissions:**
- Filesystems built to user-writable location (no sudo needed for daemon operation)
- If using `/opt/aegis`, script requests `sudo` to create directory and set permissions

**Other requirements:**
- Requires KVM access (`/dev/kvm` must be readable by the daemon process)
- Firecracker binary must be in `$PATH` (see "Installing Firecracker on Linux" below)
- The daemon is normally started with `sudo` (via `make start` or `sudo ./bin/aegis start`). Thanks to `SUDO_USER` detection, it will automatically find kernels and images in your normal user's `~/.aegis/firecracker/` directory.
- Firecracker socket files are created under `~/.aegis/state/`

#### macOS/Windows (Docker)
- Sandboxes run in Docker with resource limits
- Each sandbox gets an isolated network (aegis-net-*)
- Storage mounted from Docker volumes
- No Firecracker kernel/rootfs needed

## Configuration

### Environment Variables

```bash
# Override socket path
export AEGIS_SOCKET=~/.aegis/daemon.sock

# Override state directory
export AEGIS_STATE_DIR=~/.aegis/state

# Linux only: Override kernel path (MUST be a minimal Firecracker vmlinux, not a full vmlinuz)
export AEGIS_KERNEL_PATH=~/.aegis/firecracker/vmlinux

# Linux only: Override rootfs directory
export AEGIS_ROOTFS_DIR=~/.aegis/firecracker/rootfs
```

### Configuration Files

- **Daemon**: `~/.aegis/daemon.pid` (created automatically)
- **Logs**: `~/.aegis/daemon.log`
- **State**: `~/.aegis/state/`
- **ACLs**: `config/acls.yaml`

## Troubleshooting

### Daemon won't start

```bash
# Check health
make doctor

# Check logs
tail -f ~/.aegis/daemon.log

# Verify daemon is not already running
ps aux | grep aegis
```

### Permission errors

**Build fails with "Permission denied" when creating `/opt/aegis`:**
```bash
# The build script will automatically fall back to ~/.aegis/firecracker/rootfs
# No action needed - filesystems will be built to user-writable location

# If you want to use /opt/aegis and have sudo access:
export AEGIS_ROOTFS_DIR=/opt/aegis/firecracker/rootfs
make build-microvms
# Script will request sudo if needed to create directory
```

**For daemon operations (starting/stopping VMs):**
```bash
# Always use sudo - daemon needs elevated privileges for Firecracker
make start        # Uses sudo automatically
sudo ./bin/aegis start
```

**To enable passwordless sudo (recommended for development):**

Allow passwordless sudo for the aegis binary (recommended for daily development):

```bash
# Add to sudoers (sudo visudo):
yourusername ALL=(ALL) NOPASSWD: /path/to/bin/aegis, /path/to/scripts/build-microvms-docker.sh
```

### Supply-Chain & Release (7.8)
- `make sbom` — produces CycloneDX JSON (via cyclonedx-gomod or syft) or a high-quality fallback manifest with Builder gates + spec cross-refs.
- Image signing hooks (cosign, keyless or COSIGN_* env) are present as non-fatal placeholders in Makefile and build scripts.
- All changes are additive and preserve the sacred `make start/stop/test/test-chaos` and doctor behavior (AGENTS.md).
- References: threat-model.md:3 (backdoored skill mitigation), additional-requirements-and-gaps.md, builder-security-gates.md, grok-build-execution-plan.md:7.8.

See `make help` and the SBOM target for details.


### Installing Firecracker on Linux (Required)

The `apt` package for Firecracker is usually missing or extremely outdated. Use the official static release instead:

```bash
# 1. Download a recent release (example uses v1.15.1)
cd ~/Downloads
wget https://github.com/firecracker-microvm/firecracker/releases/download/v1.15.1/firecracker-v1.15.1-x86_64.tgz

# 2. Extract
tar -zxvf firecracker-v1.15.1-x86_64.tgz

# 3. Install into a clean, versioned location (recommended pattern)
sudo mkdir -p /usr/local/firecracker/v1.15.1
sudo cp release-v1.15.1-x86_64/firecracker-v1.15.1-x86_64 /usr/local/firecracker/v1.15.1/firecracker
sudo cp release-v1.15.1-x86_64/jailer-v1.15.1-x86_64     /usr/local/firecracker/v1.15.1/jailer
sudo chmod +x /usr/local/firecracker/v1.15.1/firecracker /usr/local/firecracker/v1.15.1/jailer

# 4. Create symlinks so `firecracker` and `jailer` are in PATH
sudo ln -sf /usr/local/firecracker/v1.15.1/firecracker /usr/local/bin/firecracker
sudo ln -sf /usr/local/firecracker/v1.15.1/jailer     /usr/local/bin/jailer

# 5. Verify
which firecracker
firecracker --version
which jailer
```

### Installing a Proper Minimal Firecracker Kernel (Critical)

**Do not use your host kernel** (`/boot/vmlinuz-*`). It is too large and incompatible, and will cause Firecracker to start the process but then fail with "connection refused" on the API socket (as seen in recent logs).

Use the helper script instead:

```bash
# Download a small, Firecracker-optimized vmlinux
./scripts/download-firecracker-kernel.sh
```

This installs a minimal kernel to `~/.aegis/firecracker/vmlinux` (the new recommended default location).

You can also download one manually:

```bash
mkdir -p ~/.aegis/firecracker
curl -fsSL -o ~/.aegis/firecracker/vmlinux \
  https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/x86_64/kernels/vmlinux.bin
chmod 644 ~/.aegis/firecracker/vmlinux
```

Then update your wrapper (or export the variable):

```bash
export AEGIS_KERNEL_PATH=~/.aegis/firecracker/vmlinux
```

A full wrapper is no longer required in most cases (see the "Pro tip" below). You only need one if you want to force specific paths or always enable debug logging.

### MicroVM/Firecracker issues (Linux)

```bash
# Check KVM access
test -r /dev/kvm && echo "KVM available" || echo "KVM not available"

# Check firecracker installation
which firecracker
firecracker --version

# Check that the daemon sees the images
ls -lh ~/.aegis/firecracker/rootfs/*.img

# Download a proper minimal kernel (highly recommended)
./scripts/download-firecracker-kernel.sh

# Rebuild microVM filesystems
make build-microvms
```

**Pro tip for daily use:** For the simplest experience, just run:

```bash
sudo ./bin/aegis start
```

(or `sudo make start`).

The daemon will automatically discover kernels and images in the *original user's* `~/.aegis/firecracker/` directory thanks to `SUDO_USER` detection. You only need a wrapper if you want to force specific paths, enable debug logging, or put the binary in a non-standard location.

### Control Socket & Running CLI Commands as a Normal User

The daemon often runs as root (required for Firecracker). The control socket lives at `/tmp/aegis/daemon.sock` (chosen so both root and normal users can reach it).

- The daemon now creates this socket with `0666` permissions (and chowns it to the original invoking user via `SUDO_USER`).
- This allows normal users to run `./bin/aegis status`, `./bin/aegis vm list`, etc. without sudo after the daemon is started.
- **Note:** Because the socket is world-writable, any local user can currently send basic commands (including stop). For production use you may want to tighten this (e.g. 0660 + a dedicated group) or add simple UID checks in the handler.

If you see "unable to query live state" or "Daemon not running or socket error" even after the daemon has printed "daemon started", wait 10–15 seconds (real microVMs take time to boot) and try again. The "daemon started" message is now tied to both the PID file *and* the control socket being ready.

**Note on real microVMs for base infrastructure:** The Dockerfiles for `store`, `network-boundary`, and `web-portal` now include a minimal `/init` script. After running `make build-microvms` (or the download kernel script + rebuild), the generated `.img` files should be more suitable for booting as real Firecracker guests.

### Firecracker boot diagnostics (2026-05+ improvements)

If you still see "Firecracker process started for VM ..., PID NNN" followed by "failed to configure VM: dial unix ... connection refused" (or the socket wait error), the daemon now automatically does the following on failure:

- Writes a detailed VMM log to `~/.aegis/state/fc-<name>.log` (or `/root/.aegis/state/...` when running via sudo).
- Writes the full guest serial console (kernel boot + /init output) to `fc-<name>-console.log` in the same directory.
- On the next failure, the daemon logs the tail of both files directly into `daemon.log` (look for "Firecracker VMM log tail" and "Guest console output").

After a failed start:

```bash
# From the machine running the daemon (use sudo if state is under /root)
sudo tail -200 /root/.aegis/daemon.log | grep -A 100 -E "(VMM log tail|Guest console|Failed to start VM)"

# Or inspect the artifacts directly
sudo ls -l /root/.aegis/state/fc-network-boundary*
sudo cat /root/.aegis/state/fc-network-boundary.log | tail -100
sudo cat /root/.aegis/state/fc-network-boundary-console.log | tail -100
```

Common Firecracker errors we hit during development (and their usual causes):

| Symptom | Most Common Cause | Fix |
|---------|-------------------|-----|
| `exec: "firecracker": executable file not found` | Firecracker not in `$PATH` for root | Use the versioned layout + symlinks or wrapper |
| `connection refused` on `fc-*.sock` right after process starts | Wrong kernel (distro vmlinuz instead of minimal vmlinux) or unbootable rootfs | Use `scripts/download-firecracker-kernel.sh` + fresh `make build-microvms` |
| `FailedToBindSocket` (socket already in use) | Stale `.sock` from previous crashed attempt | Daemon now aggressively cleans on next `Start()`; manual `rm -f /tmp/aegis/daemon.sock` also works |
| `invalid type: integer `9000`, expected a string` (vsock) | Old vsock JSON shape (Firecracker main is strict) | We now emit `"vsock_id": "string"`, `"guest_cid": N`, `"uds_path": "..."` |
| `missing field 'uds_path'` | Same as above (newer main builds require it) | Fixed in current code |
| `The requested operation is not supported after starting the microVM` (InstanceStart 400) | Sending `InstanceStart` action when using a complete `--config-file` (which auto-starts the VM) | We no longer send `InstanceStart` after a full config file |
| Kernel "No such file or directory" when running via sudo | The daemon is looking in `/root/.aegis/...` instead of your normal user's home | Fixed in current code via `SUDO_USER` detection (no wrapper needed for paths) |
| Web portal returns `{"error":"web portal temporarily unavailable"}` | The reverse proxy is still wired for the old thin host-child web-portal. Real microVM web-portal support is in progress. | The VM itself boots; only the presentation proxy needs wiring updates |

After any change to Dockerfiles or the build script, always re-run `make build-microvms` so the raw `.img` files contain the latest `/init` and binaries.

### Docker issues (macOS/Windows)

```bash
# Ensure Docker is running
docker ps

# Check Docker network
docker network ls

# Verify Docker daemon socket
test -e /var/run/docker.sock && echo "Socket available"
```

## Documentation

- **PRD**: See `docs/prd/` for product requirements and architecture
- **Specs**: See `docs/specs/` for detailed technical specifications
- **Architecture**: See `docs/architecture.md` for system design
- **Glossary**: See `docs/prd/glossary.md` for key terms

## Development

See `docs/implementation-plan/` for current development tasks.

## Roadmap

See `docs/roadmap.md` for the full development roadmap.

## License

See LICENSE file for details.