#!/bin/bash
set -euo pipefail

# build-rootfs.sh — Build a minimal Alpine ext4 rootfs for AegisClaw Firecracker sandboxes.
# The rootfs includes busybox and the guest-agent binary as PID 1.
#
# Requirements: root privileges, debootstrap or apk-tools, e2fsprogs
# Usage: sudo ./scripts/build-rootfs.sh [output-path]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUTPUT="${1:-/var/lib/aegisclaw/rootfs-templates/alpine.ext4}"
ROOTFS_SIZE_MB=256
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

# Build guest-agent binary (static, no CGO)
echo ">>> Building guest-agent binary..."
GUEST_AGENT_BIN="${WORKDIR}/guest-agent"
(
    cd "${PROJECT_ROOT}"
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
        go build -ldflags="-s -w" -o "${GUEST_AGENT_BIN}" ./cmd/guest-agent/
)
echo "Guest-agent binary: $(ls -lh "${GUEST_AGENT_BIN}" | awk '{print $5}')"

# Create ext4 image
echo ">>> Creating ext4 image (${ROOTFS_SIZE_MB} MB)..."
dd if=/dev/zero of="${WORKDIR}/rootfs.ext4" bs=1M count=${ROOTFS_SIZE_MB} status=progress
mkfs.ext4 -F -q -L aegisclaw "${WORKDIR}/rootfs.ext4"

# Mount image
echo ">>> Mounting rootfs image..."
mkdir -p "${MOUNTPOINT}"
mount -o loop "${WORKDIR}/rootfs.ext4" "${MOUNTPOINT}"

# Install Alpine minimal root filesystem
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

# Create essential directories
echo ">>> Setting up filesystem structure..."
mkdir -p "${MOUNTPOINT}"/{dev,proc,sys,tmp,run,workspace,sbin,etc}
mkdir -p "${MOUNTPOINT}/run/secrets"

# Install guest-agent as PID 1
echo ">>> Installing guest-agent..."
cp "${GUEST_AGENT_BIN}" "${MOUNTPOINT}/sbin/guest-agent"
chmod 755 "${MOUNTPOINT}/sbin/guest-agent"

# Create init symlink so kernel's init= parameter works
ln -sf /sbin/guest-agent "${MOUNTPOINT}/init"

# Configure DNS
cat > "${MOUNTPOINT}/etc/resolv.conf" << 'EOF'
nameserver 10.0.0.1
EOF

# Configure hostname
echo "aegisclaw-sandbox" > "${MOUNTPOINT}/etc/hostname"

# Set up minimal /etc/passwd and /etc/group
cat > "${MOUNTPOINT}/etc/passwd" << 'EOF'
root:x:0:0:root:/workspace:/bin/sh
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

# Make rootfs read-only friendly (workspace is on separate drive)
# Set restrictive permissions on system directories
chmod 755 "${MOUNTPOINT}"
chmod 1777 "${MOUNTPOINT}/tmp"
chmod 700 "${MOUNTPOINT}/run/secrets"

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

FINAL_SIZE=$(ls -lh "${OUTPUT}" | awk '{print $5}')
echo ""
echo "=== Rootfs build complete ==="
echo "Output: ${OUTPUT}"
echo "Size: ${FINAL_SIZE}"
echo "Guest-agent: /sbin/guest-agent (PID 1)"
echo "Workspace: /workspace (separate drive)"
echo "Secrets: /run/secrets (tmpfs, never persisted)"
