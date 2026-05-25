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
- Linux 5.10+ with KVM support
- Firecracker (minimal: 0.24.x)
- Go 1.22+
- Docker (for building microVM filesystems)
- `sudo` configured for passwordless execution of firecracker

### macOS/Windows (Docker Sandbox)
- Docker Desktop (or equivalent)
- Go 1.22+
- `sudo` privileges if using Firecracker on Linux

## Quick Start

### 1. Install Dependencies

```bash
# On Linux with Debian/Ubuntu:
sudo apt-get install -y firecracker docker.io go-tools

# On macOS:
brew install docker go

# On Windows:
# Install Docker Desktop from https://www.docker.com/products/docker-desktop
```

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
- Requires KVM access: `/dev/kvm` must be readable
- Firecracker daemon needs sudo for VM operations (handled by `make start`)
- Firecracker socket files created in `~/.aegis/state/`

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

# Linux only: Override kernel path
export AEGIS_KERNEL_PATH=/opt/aegis/firecracker/vmlinux

# Linux only: Override rootfs directory
export AEGIS_ROOTFS_DIR=/opt/aegis/firecracker/rootfs
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

**To enable passwordless sudo for specific commands:**
```bash
# Add to sudoers (sudo visudo):
%wheel ALL=(ALL) NOPASSWD: /path/to/firecracker
%wheel ALL=(ALL) NOPASSWD: /path/to/aegis
```


### MicroVM/Firecracker issues (Linux)

```bash
# Check KVM access
test -r /dev/kvm && echo "KVM available" || echo "KVM not available"

# Check firecracker installation
which firecracker

# Rebuild microVM filesystems
make build-microvms
```

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