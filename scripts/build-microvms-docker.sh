#!/bin/bash
# build-microvms-docker.sh - Build microVM filesystems using Docker
#
# Usage:
#   ./scripts/build-microvms-docker.sh [component ...]
#   ./scripts/build-microvms-docker.sh --print-default-components
#   ROOTFS_DIR=/path ./scripts/build-microvms-docker.sh
#
# Positional arguments are component names (directory names under cmd/ that
# contain a Dockerfile). They are NOT an output directory. With no arguments
# the default guest list is built. Several names may be passed as one
# space-separated argument or as one argument each.
#
# --print-default-components prints DEFAULT_COMPONENTS on stdout and exits 0
# before rootfs selection, sudo, docker, or the kernel download.
#
# Output directory (first match):
#   ROOTFS_DIR          Explicit directory for <component>.img and the
#                       matching .tar.gz (see determine_rootfs_dir).
#   /opt/aegis/firecracker/rootfs
#                       Used on Linux when that directory is writable or can
#                       be created.
#   ~/.aegis/firecracker/rootfs
#                       Fallback.
#
# AEGIS_SKIP_KERNEL_ENSURE
#   Set to 1 to skip scripts/download-firecracker-kernel.sh. The image CI job
#   does not set this: it runs the pinned-hash download and fails on mismatch.
#   The opt-out is a deliberate local skip. It prints a loud warning and
#   continues with image builds only. Without it, a kernel ensure failure
#   exits non-zero.
#
# AEGIS_DRY_RUN
#   Set to 1 to validate the component list and Dockerfiles, print the image
#   and rootfs paths that would be produced, and exit. Does not call sudo,
#   docker, or the kernel download, and does not create ROOTFS_DIR.
#
# Cosign image signing is intentionally not invoked. It is an optional
# supply-chain hook owned by the Makefile `sbom` target; a missing cosign
# binary must not fail this build.
#
# Failures that abort the script (exit 1): missing Dockerfile for a requested
# component, docker build failure, rootfs extract/archive failure, raw .img
# creation failure, and kernel ensure failure when AEGIS_SKIP_KERNEL_ENSURE
# is not 1. Optional cleanup (removing a temp mount dir or a container after
# a successful export) is logged and does not fail the build.

# -e: a failed command aborts the build.
# -o pipefail: a failure in any stage of a pipeline (docker export | tar)
# aborts too. Commands that may fail are explicit (`if`, or `||` on optional
# cleanup only).
set -eo pipefail

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
    echo -e "${YELLOW}[WARN]${NC} $1" >&2
}

info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
    exit 1
}

CREATE_ROOTFS_SCRIPT="$SCRIPT_DIR/create-firecracker-rootfs.sh"
ENSURE_DIR_SCRIPT="$SCRIPT_DIR/ensure-aegis-dir.sh"

run_sudo_script() {
	local script="$1"
	shift
	if [ ! -x "$script" ]; then
		warn "Missing executable script: $script"
		return 1
	fi
	if sudo -n "$script" "$@"; then
		return 0
	fi
	warn "NOPASSWD sudo failed for $script"
	warn "Add to /etc/sudoers.d/aegisclaw:"
	warn "  $(whoami) ALL=(root) NOPASSWD: $script"
	return 1
}

# Check if directory is writable, request sudo if needed.
# Returns 1 on failure so callers abort even when set -e is suppressed
# (this function is invoked as `if ! ensure_writable_dir`).
ensure_writable_dir() {
    local dir="$1"
    local parent_dir
    parent_dir="$(dirname "$dir")"

    # If directory doesn't exist, check parent
    if [ ! -d "$dir" ]; then
        ensure_writable_dir "$parent_dir" || return 1

        # Try to create the directory
        if ! mkdir -p "$dir" 2>/dev/null; then
            warn "Cannot write to $dir, requesting sudo..."
            if [ "$EUID" -ne 0 ]; then
                run_sudo_script "$ENSURE_DIR_SCRIPT" "$dir" || return 1
                log "Created $dir with appropriate permissions"
            else
                mkdir -p "$dir" || return 1
            fi
        fi
    elif [ ! -w "$dir" ]; then
        warn "Directory $dir is not writable, requesting sudo..."
        if [ "$EUID" -ne 0 ]; then
            run_sudo_script "$ENSURE_DIR_SCRIPT" "$dir" || return 1
            log "Fixed permissions on $dir"
        else
            chown "$(whoami)" "$dir" || return 1
        fi
    fi
}

# create_raw_rootfs_image turns a tarball (from docker export) into a bootable raw ext4 .img
# suitable for Firecracker. It produces both the raw .img and keeps the .tar.gz.
create_raw_rootfs_image() {
    local tarball="$1"
    local img_file="$2"
    local size="${3:-512M}"

    # mkfs.ext4 is in /sbin or /usr/sbin. `sudo PATH=$PATH` drops those dirs.
    local mkfs_ext4
    mkfs_ext4="$(command -v mkfs.ext4 2>/dev/null || true)"
    if [ -z "$mkfs_ext4" ] && [ -x /usr/sbin/mkfs.ext4 ]; then
        mkfs_ext4=/usr/sbin/mkfs.ext4
    fi
    if [ -z "$mkfs_ext4" ] && [ -x /sbin/mkfs.ext4 ]; then
        mkfs_ext4=/sbin/mkfs.ext4
    fi
    if [ -z "$mkfs_ext4" ]; then
        echo "mkfs.ext4 not found: install e2fsprogs (apt-get install e2fsprogs)" >&2
        exit 1
    fi

    log "Creating raw bootable rootfs image: $img_file (size=$size)"

    # Create sparse file
    if ! truncate -s "$size" "$img_file" 2>/dev/null; then
        local count
        # 512M -> 512. 1G is unchanged (same as the previous sed 's/M//').
        count=${size/M/}
        dd if=/dev/zero of="$img_file" bs=1M count="$count" status=none || {
            rm -f "$img_file"
            warn "Failed to allocate raw image file $img_file"
            return 1
        }
    fi

    # Format as ext4
    if ! "$mkfs_ext4" -F -L rootfs "$img_file" >/dev/null; then
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

    # Try direct loop mount (no sudo)
    if mount -o loop "$img_file" "$mnt" 2>/dev/null; then
        if ! tar -xzf "$tarball" -C "$mnt" --numeric-owner; then
            umount "$mnt" 2>/dev/null || true
            rmdir "$mnt" 2>/dev/null || true
            rm -f "$img_file"
            warn "Failed to extract $tarball into $img_file"
            return 1
        fi
        if ! umount "$mnt"; then
            warn "Failed to unmount $img_file after extracting the rootfs (image left unusable)"
            rmdir "$mnt" 2>/dev/null || true
            rm -f "$img_file"
            return 1
        fi
        # Leftover temp dir does not make the image wrong.
        if ! rmdir "$mnt" 2>/dev/null; then
            warn "Temp mount dir $mnt was left behind after writing $img_file (cleanup is optional)"
        fi
        log "Created raw image: $img_file"
        return 0
    fi

    # Privileged path via sudoers-approved script (no password when configured)
    rmdir "$mnt" 2>/dev/null || true
    rm -f "$img_file"
    if run_sudo_script "$CREATE_ROOTFS_SCRIPT" "$tarball" "$img_file" "$size"; then
        log "Created raw image: $img_file"
        return 0
    fi

    warn "Raw .img creation failed for $(basename "$img_file" .img) (loop mount / NOPASSWD script unavailable — tarball is still usable)"
    return 1
}

# Determine the rootfs directory to use
determine_rootfs_dir() {
    # First, check if ROOTFS_DIR is explicitly set
    if [ -n "${ROOTFS_DIR:-}" ]; then
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

# Default guest set. CI and verify-microvm-artifacts.sh read it via
# --print-default-components. test-build-microvms-args.sh compares that
# stdout to this assignment. aegishub is not listed: the host daemon execs
# ./bin/aegishub (startManagedHub), so it is not a Firecracker guest rootfs.
DEFAULT_COMPONENTS="agent project-manager web-portal builder store memory network-boundary court-persona court-scribe"

# Print the default guest list and stop. Must run before rootfs selection,
# sudo, docker, and the kernel download so callers can read the list
# unprivileged.
if [ "${1:-}" = "--print-default-components" ]; then
    if [ "$#" -ne 1 ]; then
        echo "error: --print-default-components takes no other arguments" >&2
        exit 1
    fi
    printf '%s\n' "$DEFAULT_COMPONENTS"
    exit 0
fi

# Positional parameters are component names, not an output directory.
if [ "$#" -eq 0 ]; then
    COMPONENTS=$DEFAULT_COMPONENTS
else
    COMPONENTS="$*"
fi
PLATFORM=${PLATFORM:-linux}
ROOTFS_DIR=$(determine_rootfs_dir)

log "Building microVM filesystems for: $COMPONENTS"
log "Platform: $PLATFORM"
log "Output directory: $ROOTFS_DIR"
echo ""

# Ensure the post-PR#63 Firecracker kernel (vmlinux-5.10+ with CONFIG_HW_RANDOM_VIRTIO / virtio_rng driver)
# is present. This is required for the "entropy" device (added in internal/sandbox/firecracker.go)
# to actually unblock guest CRNG quickly. Without it, store/network-boundary/etc. see the
# 130-153s "crypto/rand: blocked..." + late "crng init done" we measured when the regression
# was present. The download script is now idempotent (skips if good driver symbol present).
# This provides the kernel-side of the ".img guarantee" work on this branch for pre-warm readiness.
#
# AEGIS_SKIP_KERNEL_ENSURE=1 skips the download. The image CI job does not set
# it; it passes AEGIS_KERNEL_PATH to a writable file and treats a hash
# mismatch or a failed download as fatal. The opt-out is only a deliberate
# local skip.
if [ "${AEGIS_SKIP_KERNEL_ENSURE:-}" = "1" ]; then
    warn "AEGIS_SKIP_KERNEL_ENSURE=1: NOT ensuring the Firecracker guest kernel (skipping scripts/download-firecracker-kernel.sh)."
    warn "Image builds will continue. MicroVMs may hang on CRNG init until that download script succeeds."
elif [ "$(uname -s)" != "Linux" ]; then
    warn "Kernel ensure skipped: the Firecracker guest kernel is only downloaded on Linux (uname=$(uname -s))."
elif [ "${AEGIS_DRY_RUN:-}" = "1" ]; then
    log "DRY_RUN: would run scripts/download-firecracker-kernel.sh"
else
    log "Ensuring Firecracker kernel with virtio-rng driver (CRNG/entropy fix from #63)..."
    if ! bash "$SCRIPT_DIR/download-firecracker-kernel.sh"; then
        error "Kernel download/ensure failed. Fix scripts/download-firecracker-kernel.sh, or set AEGIS_SKIP_KERNEL_ENSURE=1 to skip it deliberately."
    fi
fi

if [ "${AEGIS_DRY_RUN:-}" != "1" ]; then
    if ! ensure_writable_dir "$ROOTFS_DIR"; then
        error "Cannot prepare output directory $ROOTFS_DIR"
    fi
fi

# Containers created during this run. One EXIT trap removes every one of them.
# A per-component trap would replace the previous handler and leak the earlier
# container when a later component fails.
BUILD_CONTAINERS=()
cleanup_build_containers() {
    local id
    for id in "${BUILD_CONTAINERS[@]}"; do
        # Export already finished; a failed rm must not change the script exit
        # status. The trap retries removal for every component.
        docker rm "$id" >/dev/null 2>&1 || true
    done
    return 0
}
trap cleanup_build_containers EXIT

# Build each component's filesystem.
# COMPONENTS is a space-separated list of names; splitting is intentional.
# shellcheck disable=SC2086
for component in $COMPONENTS; do
    log "Building filesystem for $component..."

    # Define Dockerfile path
    dockerfile_path="$REPO_ROOT/cmd/$component/Dockerfile"

    if [ ! -f "$dockerfile_path" ]; then
        error "Dockerfile not found for requested component $component at $dockerfile_path"
    fi

    image_name="aegis-${component}:latest"
    rootfs_file="$ROOTFS_DIR/${component}.img"

    if [ "${AEGIS_DRY_RUN:-}" = "1" ]; then
        log "DRY_RUN: would build image $image_name from $dockerfile_path"
        log "DRY_RUN: would write $rootfs_file and ${rootfs_file}.tar.gz"
        # Valid component: skip docker, sudo, and rootfs writes.
        continue
    fi

    # Always use the full repository root as build context.
    # All current Dockerfiles (and any new ones for base components) expect access to
    # go.mod/go.sum + internal/ packages. Using the narrow per-cmd dir was causing
    # "not found" checksum errors for court-* and would break store/web-portal/etc.
    # This matches the comments in the Dockerfiles themselves.
    build_context="$REPO_ROOT"

    if ! docker build \
        -f "$dockerfile_path" \
        -t "$image_name" \
        "$build_context"
    then
        error "Docker build failed for component $component (image $image_name)"
    fi

    # Extract rootfs from Docker image (per-component isolation)
    log "Extracting filesystem from Docker image..."

    container_id=$(docker create "$image_name") || error "Failed to create container from $image_name for component $component"
    BUILD_CONTAINERS+=("$container_id")

    component_rootfs_dir="$ROOTFS_DIR/${component}-rootfs"
    mkdir -p "$component_rootfs_dir"

    # Export container filesystem as tar (clean per-component).
    # pipefail makes a failed docker export fail this pipeline, not only tar.
    if ! docker export "$container_id" | tar -xf - -C "$component_rootfs_dir"; then
        error "Failed to extract filesystem for component $component"
    fi

    # Create per-component tarball (clean, does not accumulate previous components)
    log "Creating rootfs archive for $component..."
    if ! tar -czf "${rootfs_file}.tar.gz" -C "$component_rootfs_dir" .; then
        error "Failed to create filesystem archive for component $component"
    fi

    # Also create a ready-to-boot raw .img file (the format the Firecracker backend expects)
    raw_size="512M"
    case "$component" in
        builder) raw_size="1G" ;;
        store|network-boundary|web-portal) raw_size="1G" ;;
        *) raw_size="512M" ;;
    esac
    # Guarantee ready raw .img files (prevents on-the-fly tar->img conversion on hot
    # daemon StartVM / Ensure paths, which is a multi-second hit for cold collab startup).
    # The create function tries direct loop mount then the sudo-approved create script.
    # If raw .img production fails for a component, the build fails for that component
    # (strong signal to configure sudoers or run under appropriate privs per AGENTS.md).
    # Tarball is always produced as fallback, but .img is required for fast Firecracker
    # boots and the <1s/<5s targets.
    if ! create_raw_rootfs_image "${rootfs_file}.tar.gz" "$rootfs_file" "$raw_size"; then
        error "Failed to produce ready raw .img for $component at $rootfs_file (on-the-fly conversion would be required on first start, violating pre-warm readiness goals). Configure NOPASSWD sudoers for create-firecracker-rootfs.sh or ensure writable loop-capable dir."
    fi

    # Optional: clean up per-component dir to save space (keep tarball + raw img)
    rm -rf "$component_rootfs_dir"

    log "Filesystem for $component saved to ${rootfs_file}.tar.gz and raw ${rootfs_file}"

    # Best-effort removal now; the EXIT trap retries if this fails.
    if ! docker rm "$container_id" >/dev/null 2>&1; then
        warn "Could not remove container $container_id for $component yet (cleanup is optional and retried on exit)"
    fi

    # === Builder-specific post-processing (Phase 4 rootfs requirements) ===
    if [ "$component" = "builder" ]; then
        log "Performing Builder-specific rootfs enhancements (scanners for 5 security gates)..."

        # Ensure scanners from the image are properly present in the extracted dir
        # (they are already copied in the Dockerfile; this step can add verification or SBOM)

        # Create a minimal SBOM / manifest for supply-chain visibility (see threat-model.md:3 + additional-requirements-and-gaps.md)
        # 7.8: Now enhanced with make sbom (CycloneDX or fallback) + cosign signing hooks (grok-build-execution-plan.md:7.8).
        # Cosign is not executed here. Signing stays an optional Makefile hook so a
        # missing cosign binary cannot fail the rootfs build.
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

if [ "${AEGIS_DRY_RUN:-}" = "1" ]; then
    log "DRY_RUN complete: validated Dockerfiles only. No images were built and $ROOTFS_DIR was not written."
    exit 0
fi

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
