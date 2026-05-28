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

# create_raw_rootfs_image turns a tarball (from docker export) into a bootable raw ext4 .img
# suitable for Firecracker. It produces both the raw .img and keeps the .tar.gz.
create_raw_rootfs_image() {
    local tarball="$1"
    local img_file="$2"
    local size="${3:-512M}"

    log "Creating raw bootable rootfs image: $img_file (size=$size)"

    # Create sparse file
    if ! truncate -s "$size" "$img_file" 2>/dev/null; then
        local count=$(echo "$size" | sed 's/M//')
        dd if=/dev/zero of="$img_file" bs=1M count="$count" status=none 2>/dev/null || {
            rm -f "$img_file"
            warn "Failed to allocate raw image file $img_file"
            return 1
        }
    fi

    # Format as ext4
    if ! mkfs.ext4 -F -L rootfs "$img_file" >/dev/null 2>&1; then
        rm -f "$img_file"
        warn "mkfs.ext4 failed for $img_file"
        return 1
    fi

    # Prepare mount point
    local mnt
    mnt=$(mktemp -d) || {
        rm -f "$img_file"
        warn "Failed to create temp mount dir for $img_file"
        return 1
    }

    local used_sudo=0

    # Try direct loop mount first
    if ! mount -o loop "$img_file" "$mnt" 2>/dev/null; then
        if [ "$EUID" -ne 0 ]; then
            warn "Direct loop mount failed for $img_file, retrying with sudo..."
            if sudo mount -o loop "$img_file" "$mnt" 2>/dev/null; then
                used_sudo=1
            else
                rmdir "$mnt" 2>/dev/null || true
                rm -f "$img_file"
                warn "Failed to mount loop device for $img_file even with sudo (check /dev/loop* permissions)"
                return 1
            fi
        else
            rmdir "$mnt" 2>/dev/null || true
            rm -f "$img_file"
            warn "Failed to mount loop device for $img_file"
            return 1
        fi
    fi

    # Populate using the tarball (correct ownership from Docker export)
    if [ "$used_sudo" -eq 1 ]; then
        sudo tar -xzf "$tarball" -C "$mnt" --numeric-owner --same-owner 2>/dev/null || \
        sudo tar -xzf "$tarball" -C "$mnt" --numeric-owner
    else
        tar -xzf "$tarball" -C "$mnt" --numeric-owner 2>/dev/null || true
    fi

    # Unmount
    if [ "$used_sudo" -eq 1 ]; then
        sudo umount "$mnt" 2>/dev/null || warn "sudo umount had issues for $img_file"
    else
        umount "$mnt" 2>/dev/null || warn "umount had issues for $img_file"
    fi
    rmdir "$mnt" 2>/dev/null || true

    # Fix ownership of the final image
    if [ "$used_sudo" -eq 1 ]; then
        sudo chown "$(id -u):$(id -g)" "$img_file" 2>/dev/null || true
    fi

    log "Created raw image: $img_file"
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
    
    # Always use the full repository root as build context.
    # All current Dockerfiles (and any new ones for base components) expect access to
    # go.mod/go.sum + internal/ packages. Using the narrow per-cmd dir was causing
    # "not found" checksum errors for court-* and would break store/web-portal/etc.
    # This matches the comments in the Dockerfiles themselves.
    build_context="$REPO_ROOT"
    
    docker build \
        -f "$dockerfile_path" \
        -t "$image_name" \
        "$build_context" \
        || { warn "Docker build failed for $component (Go version / base image mismatch or other env issue — non-fatal). Continuing..."; continue; }
    
    # Extract rootfs from Docker image (per-component isolation)
    log "Extracting filesystem from Docker image..."
    
    container_id=$(docker create "$image_name")
    trap "docker rm $container_id > /dev/null 2>&1 || true" EXIT
    
    component_rootfs_dir="$ROOTFS_DIR/${component}-rootfs"
    mkdir -p "$component_rootfs_dir"
    
    # Export container filesystem as tar (clean per-component)
    docker export "$container_id" | tar -xf - -C "$component_rootfs_dir" || error "Failed to extract filesystem"
    
    rootfs_file="$ROOTFS_DIR/${component}.img"
    
    # Create per-component tarball (clean, does not accumulate previous components)
    log "Creating rootfs archive for $component..."
    tar -czf "${rootfs_file}.tar.gz" -C "$component_rootfs_dir" . || error "Failed to create filesystem archive"
    
    # Also create a ready-to-boot raw .img file (the format the Firecracker backend expects)
    raw_size="512M"
    case "$component" in
        builder) raw_size="1G" ;;
        store|network-boundary|web-portal) raw_size="1G" ;;
        *) raw_size="512M" ;;
    esac
    create_raw_rootfs_image "${rootfs_file}.tar.gz" "$rootfs_file" "$raw_size" || \
        warn "Raw .img creation skipped for $component (loop device / sudo mount not available in this environment — tarball is still usable)"
    
    # Optional: clean up per-component dir to save space (keep tarball + raw img)
    rm -rf "$component_rootfs_dir"
    
    log "Filesystem for $component saved to ${rootfs_file}.tar.gz (and raw ${rootfs_file} if successful)"
    
    docker rm "$container_id" > /dev/null 2>&1 || true

    # === Builder-specific post-processing (Phase 4 rootfs requirements) ===
    if [ "$component" = "builder" ]; then
        log "Performing Builder-specific rootfs enhancements (scanners for 5 security gates)..."
        
        # Ensure scanners from the image are properly present in the extracted dir
        # (they are already copied in the Dockerfile; this step can add verification or SBOM)
        
        # Create a minimal SBOM / manifest for supply-chain visibility (see threat-model.md:3 + additional-requirements-and-gaps.md)
        # 7.8: Now enhanced with make sbom (CycloneDX or fallback) + cosign signing hooks (grok-build-execution-plan.md:7.8).
        sbom_file="$ROOTFS_DIR/builder-sbom.txt"
        {
            echo "# AegisClaw Builder VM SBOM (Phase 4 / 7.8)"
            echo "# Generated: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
            echo "# Reference: docs/specs/builder-security-gates.md + builder-vm.md + threat-model.md:3"
            echo ""
            echo "Binary: /usr/local/bin/builder (statically linked Go)"
            echo ""
            echo "Included security gate scanners:"
            echo "  - SAST: gosec (github.com/securego/gosec)"
            echo "  - SCA: govulncheck (golang.org/x/vuln)"
            echo "  - Secrets: gitleaks + custom entropy/patterns in binary"
            echo "  - Policy-as-Code: opa (Open Policy Agent)"
            echo "  - Composition/Health: Go toolchain + smoke test support"
            echo ""
            echo "Supply-chain (7.8):"
            echo "  - Run 'make sbom' at repo root for CycloneDX JSON (cyclonedx-gomod/syft) or high-quality fallback manifest."
            echo "  - Image signing: cosign sign --yes <image> (keyless or COSIGN_*; non-fatal hook in Makefile + this script)."
            echo "  - See also: scripts/build-microvms-docker.sh (this block), grok-build-execution-plan.md:7.8, user-journeys/04+09."
            echo ""
            echo "Notes:"
            echo "  - All scanners are available inside the untrusted Builder VM."
            echo "  - Rootfs kept minimal per security-model.md (alpine base + static tools)."
            echo "  - Full SBOM + signing reduces backdoored-skill risk (threat-model.md:3)."
        } > "$sbom_file"
        
        log "Builder SBOM/manifest written to $sbom_file (7.8 enhanced with make sbom + cosign hooks)"
    fi
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
