#!/bin/bash
set -euo pipefail

# build-rootfs.sh — Build minimal Alpine ext4 rootfs images for AegisClaw Firecracker sandboxes.
#
# Targets:
#   (default / --target=guest)   guest-agent rootfs (sandbox VMs: agent, court, builder, skills)
#   --target=aegishub             AegisHub system microVM rootfs (sole IPC router)
#   --target=portal               Dashboard portal microVM rootfs
#
# Requirements: root privileges, e2fsprogs (mkfs.ext4, e2fsck, resize2fs)
# Usage:
#   sudo ./scripts/build-rootfs.sh [output-path]
#   sudo ./scripts/build-rootfs.sh --target=aegishub [output-path]
#   sudo ./scripts/build-rootfs.sh --target=portal [output-path]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ── Parse arguments ──────────────────────────────────────────────────────────
TARGET="guest"
POSITIONAL_ARGS=()
for arg in "$@"; do
    case "$arg" in
        --target=*)
            TARGET="${arg#--target=}"
            ;;
        *)
            POSITIONAL_ARGS+=("$arg")
            ;;
    esac
done

# Validate target
case "${TARGET}" in
    guest|aegishub|portal) ;;
    *)
        echo "ERROR: Unknown target '${TARGET}'. Valid targets: guest, aegishub, portal"
        exit 1
        ;;
esac

# Set defaults based on target
if [ "${TARGET}" = "aegishub" ]; then
    DEFAULT_OUTPUT="/var/lib/aegisclaw/rootfs-templates/aegishub-rootfs.ext4"
    ROOTFS_LABEL="aegishub"
    # AegisHub is minimal: only the router binary, no Alpine packages needed
    ROOTFS_SIZE_MB=64
    BINARY_NAME="aegishub"
    BINARY_SRC_PKG="./cmd/aegishub/"
    BINARY_DEST="/sbin/aegishub"
    HOSTNAME="aegisclaw-hub"
elif [ "${TARGET}" = "portal" ]; then
    DEFAULT_OUTPUT="/var/lib/aegisclaw/rootfs-templates/portal-rootfs.ext4"
    ROOTFS_LABEL="aegisportal"
    # Portal VM hosts only the web UI process and uses vsock for host RPC.
    ROOTFS_SIZE_MB=96
    BINARY_NAME="aegisportal"
    BINARY_SRC_PKG="./cmd/aegisportal/"
    BINARY_DEST="/sbin/aegisportal"
    HOSTNAME="aegisclaw-portal"
else
    DEFAULT_OUTPUT="/var/lib/aegisclaw/rootfs-templates/alpine.ext4"
    ROOTFS_LABEL="aegisclaw"
    ROOTFS_SIZE_MB=256
    BINARY_NAME="guest-agent"
    BINARY_SRC_PKG="./cmd/guest-agent/"
    BINARY_DEST="/sbin/guest-agent"
    HOSTNAME="aegisclaw-sandbox"
fi

OUTPUT="${POSITIONAL_ARGS[0]:-${DEFAULT_OUTPUT}}"
WORKDIR=$(mktemp -d /tmp/aegisclaw-rootfs.XXXXXX)
MOUNTPOINT="${WORKDIR}/mnt"
ALPINE_VERSION="3.21"
ALPINE_MIRROR="https://dl-cdn.alpinelinux.org/alpine"

cleanup() {
    echo "Cleaning up..."
    umount "${MOUNTPOINT}" 2>/dev/null || true
    losetup -D 2>/dev/null || true
    rm -rf "${WORKDIR}"
}
trap cleanup EXIT

echo "=== AegisClaw Rootfs Builder ==="
echo "Target: ${TARGET}"
echo "Output: ${OUTPUT}"
echo "Size: ${ROOTFS_SIZE_MB} MB"
echo "Alpine: v${ALPINE_VERSION}"
echo ""

# Check for root
if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: This script must be run as root"
    exit 1
fi

# Check dependencies
for cmd in dd mkfs.ext4 mount losetup; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "ERROR: Required command not found: $cmd"
        exit 1
    fi
done

# Build the target binary (static, no CGO)
echo ">>> Building ${BINARY_NAME} binary..."
BINARY_BIN="${WORKDIR}/${BINARY_NAME}"

# Locate go binary — sudo resets PATH, so check common install locations.
GO_BIN=""
for candidate in "$(command -v go 2>/dev/null)" /usr/local/go/bin/go /usr/lib/go/bin/go "${GOROOT:-/nonexistent}/bin/go" "${HOME}/go/bin/go" /snap/go/current/bin/go; do
    if [ -x "${candidate}" ]; then
        GO_BIN="${candidate}"
        break
    fi
done
if [ -z "${GO_BIN}" ] && [ -n "${SUDO_USER:-}" ]; then
    # Try the invoking user's GOROOT / go installation
    USER_HOME=$(getent passwd "${SUDO_USER}" | cut -d: -f6)
    for candidate in "${USER_HOME}/go/bin/go" "${USER_HOME}/.local/go/bin/go" "${USER_HOME}/sdk/go/bin/go"; do
        if [ -x "${candidate}" ]; then
            GO_BIN="${candidate}"
            break
        fi
    done
fi
if [ -z "${GO_BIN}" ]; then
    echo "ERROR: Go compiler not found. Install Go or set GOROOT."
    echo "  Common fix: export PATH=\$PATH:/usr/local/go/bin"
    exit 1
fi
echo "Using Go: ${GO_BIN}"

(
    cd "${PROJECT_ROOT}"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
        "${GO_BIN}" build -ldflags="-s -w" -o "${BINARY_BIN}" ${BINARY_SRC_PKG}
)
echo "${BINARY_NAME} binary: $(du -sh "${BINARY_BIN}" | cut -f1)"

# Create ext4 image
echo ">>> Creating ext4 image (${ROOTFS_SIZE_MB} MB)..."
dd if=/dev/zero of="${WORKDIR}/rootfs.ext4" bs=1M count=${ROOTFS_SIZE_MB} status=progress
mkfs.ext4 -F -q -L "${ROOTFS_LABEL}" "${WORKDIR}/rootfs.ext4"

# Mount image
echo ">>> Mounting rootfs image..."
mkdir -p "${MOUNTPOINT}"
mount -o loop "${WORKDIR}/rootfs.ext4" "${MOUNTPOINT}"

# ── Alpine base (for guest-agent target only) ─────────────────────────────────
# The AegisHub rootfs is intentionally bare: only busybox + the aegishub binary.
# No Alpine packages, no shell, no tools — minimal attack surface.
if [ "${TARGET}" = "guest" ]; then
    # Install Alpine minimal root filesystem for sandbox VMs
    echo ">>> Installing Alpine base..."
    if command -v apk &>/dev/null; then
        # If apk is available (running on Alpine), use it directly
        apk add --root "${MOUNTPOINT}" --initdb \
            --repository "${ALPINE_MIRROR}/v${ALPINE_VERSION}/main" \
            --no-cache \
            alpine-base busybox
    else
        # Download and extract Alpine minirootfs
        ARCH="x86_64"
        MINIROOTFS_URL="${ALPINE_MIRROR}/v${ALPINE_VERSION}/releases/${ARCH}/alpine-minirootfs-${ALPINE_VERSION}.0-${ARCH}.tar.gz"
        echo "Downloading Alpine minirootfs from ${MINIROOTFS_URL}..."
        wget -q -O "${WORKDIR}/alpine-minirootfs.tar.gz" "${MINIROOTFS_URL}"
        tar xzf "${WORKDIR}/alpine-minirootfs.tar.gz" -C "${MOUNTPOINT}"
    fi
fi

# Create essential directories
echo ">>> Setting up filesystem structure..."
mkdir -p "${MOUNTPOINT}"/{dev,proc,sys,tmp,run,sbin,etc}
if [ "${TARGET}" = "guest" ]; then
    mkdir -p "${MOUNTPOINT}/workspace"
    mkdir -p "${MOUNTPOINT}/run/secrets"
fi

# Install binary as PID 1
echo ">>> Installing ${BINARY_NAME} as ${BINARY_DEST}..."
cp "${BINARY_BIN}" "${MOUNTPOINT}${BINARY_DEST}"
chmod 755 "${MOUNTPOINT}${BINARY_DEST}"

# Create /init symlink so the kernel's init= parameter works
ln -sf "${BINARY_DEST}" "${MOUNTPOINT}/init"

# Configure hostname
echo "${HOSTNAME}" > "${MOUNTPOINT}/etc/hostname"

# No resolv.conf for AegisHub: it has DefaultDeny network policy (vsock-only).
# Other targets still need DNS for the guest agent's proxy lookups.
if [ "${TARGET}" = "guest" ]; then
    cat > "${MOUNTPOINT}/etc/resolv.conf" << 'EOF'
nameserver 10.0.0.1
EOF
fi

# Set up minimal /etc/passwd and /etc/group
cat > "${MOUNTPOINT}/etc/passwd" << 'EOF'
root:x:0:0:root:/root:/sbin/nologin
nobody:x:65534:65534:nobody:/:/sbin/nologin
EOF

cat > "${MOUNTPOINT}/etc/group" << 'EOF'
root:x:0:
nobody:x:65534:
EOF

# Remove unnecessary files to minimize image size
echo ">>> Minimizing rootfs..."
rm -rf "${MOUNTPOINT}"/var/cache/* 2>/dev/null || true
rm -rf "${MOUNTPOINT}"/usr/share/doc 2>/dev/null || true
rm -rf "${MOUNTPOINT}"/usr/share/man 2>/dev/null || true
rm -rf "${MOUNTPOINT}"/usr/share/info 2>/dev/null || true

# Set restrictive permissions
chmod 755 "${MOUNTPOINT}"
chmod 1777 "${MOUNTPOINT}/tmp"
if [ "${TARGET}" = "guest" ]; then
    chmod 700 "${MOUNTPOINT}/run/secrets"
fi

# Unmount
echo ">>> Finalizing image..."
umount "${MOUNTPOINT}"

# Shrink image
e2fsck -f -y "${WORKDIR}/rootfs.ext4" || true
resize2fs -M "${WORKDIR}/rootfs.ext4"

# Copy to output
echo ">>> Installing rootfs to ${OUTPUT}..."
mkdir -p "$(dirname "${OUTPUT}")"
cp "${WORKDIR}/rootfs.ext4" "${OUTPUT}"
chmod 444 "${OUTPUT}"

FINAL_SIZE=$(du -sh "${OUTPUT}" | cut -f1)
echo ""
echo "=== Rootfs build complete ==="
echo "Target:  ${TARGET}"
echo "Output:  ${OUTPUT}"
echo "Size:    ${FINAL_SIZE}"
echo "Binary:  ${BINARY_DEST} (PID 1)"
if [ "${TARGET}" = "guest" ]; then
    echo "Workspace: /workspace (separate drive)"
    echo "Secrets:   /run/secrets (tmpfs, never persisted)"
fi

