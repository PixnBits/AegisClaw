#!/bin/bash
# scripts/build-builder-rootfs.sh
# Builds the builder rootfs template: Alpine + Go + git + golangci-lint + staticcheck + make
#
# Prerequisites: root access, qemu-img, e2fsprogs, alpine-make-rootfs or equivalent
# Security: This script runs on the host during setup ONLY. The resulting rootfs
# is used read-only by builder sandboxes.
#
# Usage: sudo ./scripts/build-builder-rootfs.sh [output_path]

set -euo pipefail

OUTPUT="${1:-/var/lib/aegisclaw/rootfs-templates/builder.ext4}"
ROOTFS_SIZE_MB=2048
MOUNT_DIR=$(mktemp -d /tmp/builder-rootfs.XXXXXX)
GO_VERSION="1.24.4"

cleanup() {
    echo "Cleaning up..."
    umount "$MOUNT_DIR" 2>/dev/null || true
    rm -rf "$MOUNT_DIR"
}
trap cleanup EXIT

echo "=== AegisClaw Builder Rootfs Build ==="
echo "Output: $OUTPUT"
echo "Size: ${ROOTFS_SIZE_MB}MB"
echo ""

# Create output directory
mkdir -p "$(dirname "$OUTPUT")"

# Create ext4 image
echo "[1/6] Creating ext4 image..."
dd if=/dev/zero of="$OUTPUT" bs=1M count=$ROOTFS_SIZE_MB status=progress
mkfs.ext4 -F "$OUTPUT"

# Mount image
echo "[2/6] Mounting image..."
mount -o loop "$OUTPUT" "$MOUNT_DIR"

# Install Alpine base system
echo "[3/6] Installing Alpine base system..."
if command -v alpine-make-rootfs &>/dev/null; then
    alpine-make-rootfs "$MOUNT_DIR" --packages "bash openssl ca-certificates curl wget"
else
    # Manual Alpine rootfs bootstrap
    ALPINE_MIRROR="https://dl-cdn.alpinelinux.org/alpine/v3.21"
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64) ALPINE_ARCH="x86_64" ;;
        aarch64) ALPINE_ARCH="aarch64" ;;
        *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
    esac

    APK_TOOLS_URL="${ALPINE_MIRROR}/main/${ALPINE_ARCH}/apk-tools-static-2.14.6-r3.apk"
    APK_TMP=$(mktemp -d)
    curl -sL "$APK_TOOLS_URL" | tar xz -C "$APK_TMP"

    "$APK_TMP/sbin/apk.static" \
        --root "$MOUNT_DIR" \
        --initdb \
        --arch "$ALPINE_ARCH" \
        --repository "${ALPINE_MIRROR}/main" \
        --repository "${ALPINE_MIRROR}/community" \
        --no-cache \
        add alpine-base bash openssl ca-certificates curl wget

    rm -rf "$APK_TMP"
fi

# Install build tools
echo "[4/6] Installing build tools..."
chroot "$MOUNT_DIR" /bin/sh -c "
    apk add --no-cache \
        git \
        make \
        gcc \
        musl-dev \
        linux-headers
"

# Install Go
echo "[5/6] Installing Go ${GO_VERSION}..."
GOARCH="amd64"
case "$(uname -m)" in
    aarch64) GOARCH="arm64" ;;
esac

curl -sL "https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz" | \
    tar xz -C "$MOUNT_DIR/usr/local"

# Install Go tools inside chroot
chroot "$MOUNT_DIR" /bin/sh -c "
    export PATH=/usr/local/go/bin:\$PATH
    export GOPATH=/opt/go
    export GOBIN=/usr/local/bin

    # golangci-lint
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

    # staticcheck
    go install honnef.co/go/tools/cmd/staticcheck@latest

    # gosec (security scanner)
    go install github.com/securego/gosec/v2/cmd/gosec@latest

    # Clean up Go module cache to reduce image size
    rm -rf /opt/go
"

# Set up builder workspace directory and init script
echo "[6/6] Configuring builder environment..."
mkdir -p "$MOUNT_DIR/workspace"
mkdir -p "$MOUNT_DIR/etc/init.d"

cat > "$MOUNT_DIR/etc/profile.d/go.sh" << 'GOENV'
export PATH=/usr/local/go/bin:/usr/local/bin:$PATH
export GOPATH=/workspace/go
export GOCACHE=/workspace/.cache/go-build
export GOMODCACHE=/workspace/go/pkg/mod
GOENV

# Create builder agent init script
cat > "$MOUNT_DIR/etc/init.d/builder-agent" << 'AGENT'
#!/bin/sh
# Builder agent – listens on vsock for build commands from the kernel
# This will be replaced by the actual guest agent binary in production
case "$1" in
    start)
        echo "Builder agent starting..."
        ;;
    stop)
        echo "Builder agent stopping..."
        ;;
esac
AGENT
chmod +x "$MOUNT_DIR/etc/init.d/builder-agent"

# Set workspace as the only writable mount point
# (Firecracker config enforces read-only root)
echo "/dev/vdb /workspace ext4 rw,nosuid,nodev 0 0" >> "$MOUNT_DIR/etc/fstab"

# Unmount and verify
umount "$MOUNT_DIR"

echo ""
echo "=== Builder rootfs created successfully ==="
echo "Path: $OUTPUT"
echo "Size: $(du -h "$OUTPUT" | cut -f1)"
echo ""
echo "To verify: mount -o loop,ro $OUTPUT /mnt && ls /mnt/usr/local/go/bin/"
