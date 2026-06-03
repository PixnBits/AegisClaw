#!/bin/bash
# download-firecracker-kernel.sh
#
# Downloads a minimal, Firecracker-compatible vmlinux kernel.
# This is strongly preferred over using a full distro kernel (vmlinuz).
#
# Usage:
#   ./scripts/download-firecracker-kernel.sh
#   AEGIS_KERNEL_PATH=~/.aegis/firecracker/vmlinux ./bin/aegis start
#
# Re-run after code changes that affect the required kernel features (e.g. adding
# virtio-rng device support for guest entropy / #62). The downloaded kernel must
# have the virtio-rng driver built-in for the device to unblock CRNG quickly.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${GREEN}[KERNEL]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }

# Determine target location
if [ -n "$AEGIS_KERNEL_PATH" ]; then
    KERNEL_PATH="$AEGIS_KERNEL_PATH"
else
    KERNEL_PATH="${HOME}/.aegis/firecracker/vmlinux"
fi

KERNEL_DIR=$(dirname "$KERNEL_PATH")
mkdir -p "$KERNEL_DIR"

# Use a known-good minimal kernel from the Firecracker CI artifacts (v1.7 series).
# This is a small vmlinux-5.10 build that includes CONFIG_HW_RANDOM_VIRTIO=y
# (and other virtio drivers) built-in. Required for the virtio-rng device
# (added for GitHub #62) to actually feed the guest entropy pool and init CRNG
# quickly. The old quickstart vmlinux.bin (4.14) lacked the rng driver.
KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.7/x86_64/vmlinux-5.10.209"

log "Downloading minimal Firecracker kernel (with virtio-rng driver) to $KERNEL_PATH ..."

if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$KERNEL_URL" -o "$KERNEL_PATH"
elif command -v wget >/dev/null 2>&1; then
    wget -qO "$KERNEL_PATH" "$KERNEL_URL"
else
    echo "Error: neither curl nor wget found"
    exit 1
fi

chmod 644 "$KERNEL_PATH"

log "Kernel downloaded successfully."
log "Size: $(du -h "$KERNEL_PATH" | cut -f1)"

warn "You should now set:"
echo "  export AEGIS_KERNEL_PATH=$KERNEL_PATH"
echo ""
warn "Then start the daemon (re-run this script + restart after any kernel change):"
echo "  sudo -E make start"
echo ""
log "This kernel enables the virtio-rng device (see internal/sandbox/firecracker.go)"
log "so that guest CRNG init happens in seconds instead of minutes."