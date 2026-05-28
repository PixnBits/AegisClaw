#!/bin/bash
# download-firecracker-kernel.sh
#
# Downloads a minimal, Firecracker-compatible vmlinux kernel.
# This is strongly preferred over using a full distro kernel (vmlinuz).
#
# Usage:
#   ./scripts/download-firecracker-kernel.sh
#   AEGIS_KERNEL_PATH=~/.aegis/firecracker/vmlinux ./bin/aegis start

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

# Use a known-good minimal kernel from the Firecracker quickstart guide.
# This is a small, stripped vmlinux that boots reliably with Firecracker.
KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/x86_64/kernels/vmlinux.bin"

log "Downloading minimal Firecracker kernel to $KERNEL_PATH ..."

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
warn "Then start the daemon with your wrapper or directly:"
echo "  sudo /usr/local/sbin/aegis-start start"
echo ""
log "For production use, consider building your own minimal kernel from the Firecracker kernel configs for better security and size."