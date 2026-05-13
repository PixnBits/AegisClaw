#!/bin/bash
# build-microvms-docker.sh - Build microVM filesystems using Docker
# This script creates cacheable microVM filesystems for different components

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log() {
    echo -e "${GREEN}[BUILD]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Check if directory is writable, request sudo if needed
ensure_writable_dir() {
    local dir="$1"
    local parent_dir="$(dirname "$dir")"
    
    # If directory doesn't exist, check parent
    if [ ! -d "$dir" ]; then
        ensure_writable_dir "$parent_dir"
        
        # Try to create the directory
        if ! mkdir -p "$dir" 2>/dev/null; then
            warn "Cannot write to $dir, requesting sudo..."
            if [ "$EUID" -ne 0 ]; then
                sudo mkdir -p "$dir"
                sudo chown "$(id -u):$(id -g)" "$dir"
                log "Created $dir with appropriate permissions"
            else
                mkdir -p "$dir"
            fi
        fi
    else
        # Directory exists, check if writable
        if [ ! -w "$dir" ]; then
            warn "Directory $dir is not writable, requesting sudo..."
            if [ "$EUID" -ne 0 ]; then
                sudo chown "$(id -u):$(id -g)" "$dir"
                log "Fixed permissions on $dir"
            else
                chown "$(whoami)" "$dir"
            fi
        fi
    fi
}

# Determine the rootfs directory to use
determine_rootfs_dir() {
    # First, check if ROOTFS_DIR is explicitly set
    if [ -n "$ROOTFS_DIR" ]; then
        echo "$ROOTFS_DIR"
        return 0
    fi
    
    # Try system location first (Linux only)
    if [ "$(uname -s)" = "Linux" ]; then
        local sys_dir="/opt/aegis/firecracker/rootfs"
        if [ -d "$sys_dir" ] && [ -w "$sys_dir" ]; then
            echo "$sys_dir"
            return 0
        fi
        # Check if we can create it
        if [ -w "$(dirname "$sys_dir")" ] || [ "$EUID" -eq 0 ]; then
            echo "$sys_dir"
            return 0
        fi
    fi
    
    # Fall back to user home directory
    local user_dir="${HOME}/.aegis/firecracker/rootfs"
    echo "$user_dir"
}

# Parse command line arguments
COMPONENTS=${1:-"agent web-portal builder store memory network-boundary court-persona court-scribe"}
PLATFORM=${PLATFORM:-linux}
ROOTFS_DIR=$(determine_rootfs_dir)

log "Building microVM filesystems for: $COMPONENTS"
log "Platform: $PLATFORM"
log "Output directory: $ROOTFS_DIR"
echo ""

# Ensure output directory exists and is writable
ensure_writable_dir "$ROOTFS_DIR"

# Build each component's filesystem
for component in $COMPONENTS; do
    log "Building filesystem for $component..."
    
    # Define Dockerfile path
    dockerfile_path="$REPO_ROOT/cmd/$component/Dockerfile"
    
    if [ ! -f "$dockerfile_path" ]; then
        warn "Dockerfile not found for $component at $dockerfile_path, skipping"
        continue
    fi
    
    # Build Docker image
    image_name="aegis-${component}:latest"
    docker build \
        -f "$dockerfile_path" \
        -t "$image_name" \
        "$REPO_ROOT/cmd/$component" \
        || error "Failed to build Docker image for $component"
    
    # Extract rootfs from Docker image
    # This creates a minimal filesystem that can be used by Firecracker
    log "Extracting filesystem from Docker image..."
    
    container_id=$(docker create "$image_name")
    trap "docker rm $container_id > /dev/null 2>&1 || true" EXIT
    
    rootfs_file="$ROOTFS_DIR/${component}.img"
    
    # Export container filesystem as tar
    docker export "$container_id" | tar -xf - -C "$ROOTFS_DIR" || error "Failed to extract filesystem"
    
    # Create ext4 filesystem image (for Firecracker)
    # This is a simplified approach; for production, would create proper ext4 images
    log "Creating ext4 filesystem image for $component..."
    
    # For now, create a tarball that can be used to initialize the rootfs
    tar -czf "${rootfs_file}.tar.gz" -C "$ROOTFS_DIR" . || error "Failed to create filesystem archive"
    
    log "Filesystem for $component saved to ${rootfs_file}.tar.gz"
    
    docker rm "$container_id" > /dev/null 2>&1 || true
done

log "MicroVM filesystem build complete!"
log "Filesystems available at: $ROOTFS_DIR"
echo ""

# Provide configuration guidance
if [ "$ROOTFS_DIR" != "${HOME}/.aegis/firecracker/rootfs" ]; then
    info "To use these filesystems, set environment variable:"
    echo "  export AEGIS_ROOTFS_DIR=$ROOTFS_DIR"
else
    info "Filesystems will be automatically discovered at:"
    echo "  $ROOTFS_DIR"
fi
echo ""
log "Filesystem build complete! Ready for daemon startup."
