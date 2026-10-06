#!/bin/bash
# download-firecracker-kernel.sh
#
# Downloads a minimal, Firecracker-compatible vmlinux kernel.
# This is strongly preferred over using a full distro kernel (vmlinuz).
#
# Usage:
#   ./scripts/download-firecracker-kernel.sh
#   ./scripts/download-firecracker-kernel.sh --print-pinned-sha256
#   AEGIS_KERNEL_PATH=~/.aegis/firecracker/vmlinux ./bin/aegis start
#
# Environment:
#   AEGIS_KERNEL_PATH            Install path (default: ~/.aegis/firecracker/vmlinux).
#   AEGIS_KERNEL_URL             Download URL override. The pinned SHA-256 belongs to
#                                KERNEL_URL only, so an override requires
#                                AEGIS_KERNEL_SHA256 or AEGIS_SKIP_KERNEL_CHECKSUM=1.
#   AEGIS_KERNEL_SHA256          Expected SHA-256 for a custom or private-mirror kernel.
#                                Set this to the on-disk hash to accept an existing
#                                kernel that differs from the in-repo pin.
#   AEGIS_SKIP_KERNEL_CHECKSUM   Set to 1 to install or keep a kernel without verifying
#                                (air-gapped host, private mirror, or no sha256sum/shasum).
#                                Prints a loud warning. Without this flag, a missing hash
#                                tool fails the existing-kernel check.
#
# Re-run after code changes that affect the required kernel features (e.g. adding
# virtio-rng device support for guest entropy / #62). The downloaded kernel must
# have the virtio-rng driver built-in for the device to unblock CRNG quickly.

set -euo pipefail

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${GREEN}[KERNEL]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }

# sha256sum (GNU) or shasum -a 256 (macOS). Prints the lowercase hex digest.
file_sha256() {
    local file="$1"
    local sum hex
    if command -v sha256sum >/dev/null 2>&1; then
        sum=$(sha256sum -- "$file") || return 1
    elif command -v shasum >/dev/null 2>&1; then
        sum=$(shasum -a 256 -- "$file") || return 1
    else
        echo "Error: neither sha256sum nor shasum found; cannot verify kernel checksum." >&2
        return 1
    fi
    hex=${sum%% *}
    hex=$(printf '%s' "$hex" | tr '[:upper:]' '[:lower:]') || return 1
    case "$hex" in
        *[!0-9a-f]* | "")
            echo "Error: could not parse SHA-256 output for $file" >&2
            return 1
            ;;
    esac
    printf '%s\n' "$hex"
}

# Determine target location
if [ -n "${AEGIS_KERNEL_PATH:-}" ]; then
    KERNEL_PATH="$AEGIS_KERNEL_PATH"
else
    KERNEL_PATH="${HOME}/.aegis/firecracker/vmlinux"
fi

KERNEL_DIR=$(dirname "$KERNEL_PATH")

# Use a known-good minimal kernel from the Firecracker CI artifacts (v1.7 series).
# This is a small vmlinux-5.10 build that includes CONFIG_HW_RANDOM_VIRTIO=y
# (and other virtio drivers) built-in. Required for the virtio-rng device
# (added for GitHub #62) to actually feed the guest entropy pool and init CRNG
# quickly. The old quickstart vmlinux.bin (4.14) lacked the rng driver.
KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.7/x86_64/vmlinux-5.10.209"
# KERNEL_SHA256 must be updated in the same commit as KERNEL_URL (version-bump policy).
KERNEL_SHA256="932450603af9175c443f5348aa961326945e3b4b46ba34bab2c714c751ee2f85"

# --print-pinned-sha256 prints the in-repo pin and exits, so other scripts
# (verify-microvm-artifacts.sh --kernel) check against the same value.
# Environment overrides are ignored here.
if [ "${1:-}" = "--print-pinned-sha256" ]; then
    printf '%s\n' "$KERNEL_SHA256"
    exit 0
fi

if [ -n "${AEGIS_KERNEL_URL:-}" ]; then
    if [ -z "${AEGIS_KERNEL_SHA256:-}" ] && [ "${AEGIS_SKIP_KERNEL_CHECKSUM:-}" != "1" ]; then
        echo "Error: AEGIS_KERNEL_URL overrides the pinned kernel URL." >&2
        echo "Set AEGIS_KERNEL_SHA256 to that file's SHA-256, or set AEGIS_SKIP_KERNEL_CHECKSUM=1 to skip verification." >&2
        exit 1
    fi
    KERNEL_URL="$AEGIS_KERNEL_URL"
fi
if [ -n "${AEGIS_KERNEL_SHA256:-}" ]; then
    KERNEL_SHA256="$AEGIS_KERNEL_SHA256"
fi
expected=$(printf '%s' "$KERNEL_SHA256" | tr '[:upper:]' '[:lower:]')

mkdir -p "$KERNEL_DIR"

download_tmp=""
cleanup_download_tmp() {
    if [ -n "${download_tmp:-}" ] && [ -f "$download_tmp" ]; then
        rm -f -- "$download_tmp"
    fi
}

# Idempotency: if a kernel is already present and contains the virtio_rng driver
# (the key artifact from the PR #63 / #62 CRNG fix), skip the download. This
# prevents repeated full downloads during `make build-microvms` and makes it
# safe to always invoke from build flows and doctor.
if [ -f "$KERNEL_PATH" ]; then
    if grep -q 'virtio_rng' "$KERNEL_PATH" 2>/dev/null || grep -q 'virtio-rng' "$KERNEL_PATH" 2>/dev/null; then
        log "Kernel at $KERNEL_PATH already contains virtio_rng driver (post #63 fix for fast guest CRNG). Skipping download."
        if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
            if [ "${AEGIS_SKIP_KERNEL_CHECKSUM:-}" = "1" ]; then
                warn "AEGIS_SKIP_KERNEL_CHECKSUM=1: neither sha256sum nor shasum is available."
                warn "Could not compute SHA-256 of the existing kernel at $KERNEL_PATH."
                warn "Leaving the file in place (not deleted). SKIPPING SHA-256 verification."
                exit 0
            fi
            echo "Error: cannot verify the existing kernel at $KERNEL_PATH." >&2
            echo "Neither sha256sum nor shasum is available." >&2
            echo "The file was left in place (not deleted)." >&2
            echo "Set AEGIS_SKIP_KERNEL_CHECKSUM=1 to keep it without verification." >&2
            exit 1
        fi
        if ! actual=$(file_sha256 "$KERNEL_PATH"); then
            echo "Error: could not compute SHA-256 of the existing kernel at $KERNEL_PATH." >&2
            echo "The file was left in place (not deleted)." >&2
            exit 1
        fi
        # A mismatched existing kernel is not a successful skip. Leave the file
        # untouched and exit non-zero unless the operator opts in.
        if [ "$actual" != "$expected" ]; then
            if [ "${AEGIS_SKIP_KERNEL_CHECKSUM:-}" = "1" ]; then
                warn "AEGIS_SKIP_KERNEL_CHECKSUM=1: existing kernel SHA-256 does not match the pinned hash."
                warn "Expected: $expected"
                warn "Actual:   $actual"
                warn "Leaving the file in place (not deleted). SKIPPING SHA-256 verification."
                exit 0
            fi
            echo "Error: existing kernel SHA-256 does not match the pinned hash." >&2
            echo "Expected: $expected" >&2
            echo "Actual:   $actual" >&2
            echo "The file was left in place (not deleted)." >&2
            echo "To accept a deliberate custom kernel, set AEGIS_KERNEL_SHA256=$actual" >&2
            echo "or set AEGIS_SKIP_KERNEL_CHECKSUM=1." >&2
            exit 1
        fi
        log "Existing kernel SHA-256 matches the pinned hash."
        exit 0
    fi
    warn "Existing kernel at $KERNEL_PATH does not appear to include the virtio_rng driver; re-downloading the required 5.10+ kernel..."
fi

log "Downloading minimal Firecracker kernel (with virtio-rng driver) to $KERNEL_PATH ..."

# Download beside the destination so mv stays on one filesystem, and only
# replace KERNEL_PATH after the checksum matches.
download_tmp=$(mktemp "${KERNEL_DIR}/.vmlinux.download.XXXXXX")
trap cleanup_download_tmp EXIT

if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$KERNEL_URL" -o "$download_tmp"
elif command -v wget >/dev/null 2>&1; then
    wget -qO "$download_tmp" "$KERNEL_URL"
else
    echo "Error: neither curl nor wget found" >&2
    exit 1
fi

if [ "${AEGIS_SKIP_KERNEL_CHECKSUM:-}" = "1" ]; then
    warn "AEGIS_SKIP_KERNEL_CHECKSUM=1: SKIPPING SHA-256 verification of the downloaded kernel."
    warn "Installing UNVERIFIED bytes from $KERNEL_URL."
    warn "Only use this for an air-gapped host or a private mirror you already trust."
else
    if ! actual=$(file_sha256 "$download_tmp"); then
        rm -f -- "$download_tmp"
        download_tmp=""
        echo "Error: could not compute SHA-256 of the downloaded kernel; it was not installed." >&2
        exit 1
    fi
    if [ "$actual" != "$expected" ]; then
        rm -f -- "$download_tmp"
        download_tmp=""
        echo "Error: SHA-256 mismatch; downloaded kernel was not installed." >&2
        echo "Expected: $expected" >&2
        echo "Actual:   $actual" >&2
        exit 1
    fi
    log "SHA-256 verified."
fi

mv -f -- "$download_tmp" "$KERNEL_PATH"
download_tmp=""
chmod 644 -- "$KERNEL_PATH"

log "Kernel downloaded successfully."
log "Size: $(du -h "$KERNEL_PATH" | cut -f1)"

warn "You should now set:"
echo "  export AEGIS_KERNEL_PATH=$KERNEL_PATH"
echo ""
warn "Then start the daemon (re-run this script + restart after any kernel change):"
echo "  sudo ./bin/aegis start --foreground"
echo ""
log "This kernel enables the virtio-rng device (see internal/sandbox/firecracker.go)"
log "so that guest CRNG init happens in seconds instead of minutes."
