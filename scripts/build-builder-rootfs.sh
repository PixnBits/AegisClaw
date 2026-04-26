#!/bin/bash
# scripts/build-builder-rootfs.sh
# AegisClaw Builder Rootfs Builder — v2 (Proposal 50)

set -euo pipefail

CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/aegisclaw-builder"

usage() {
    cat <<EOF
Usage: $0 <output-image-path>
Builds a builder rootfs with Go toolchain + linters.
EOF
    exit 1
}

[ $# -eq 1 ] || usage

OUTPUT_IMAGE="$1"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
WORKDIR="$(mktemp -d -t aegisclaw-builder-workdir-XXXXXX)"
MOUNT="$(mktemp -d -t aegisclaw-builder-mount-XXXXXX)"
trap 'sudo umount "$MOUNT" 2>/dev/null || true; sudo rm -rf "$MOUNT" "$WORKDIR"' EXIT

echo "=== AegisClaw Builder Rootfs Build ==="
echo "Output: $OUTPUT_IMAGE"
echo "Cache:  $CACHE_DIR"

# ── setup_builder (centralized, cached, mirrored) ───────────────────────────
setup_builder() {
    echo ">>> Installing Alpine base + build tools (via Docker)..."

    mkdir -p "$CACHE_DIR/apk" "$CACHE_DIR/go"

    docker run --rm \
        --network host \
        -v "$MOUNT:/target" \
        -v "$CACHE_DIR/apk:/var/cache/apk" \
        -v "$CACHE_DIR/go:/root/go/pkg/mod" \
        alpine:3.21 sh -c '
set -euo pipefail

# Mirror fallback + retry
echo "Setting up APK mirrors..."
for mirror in dl-cdn.alpinelinux.org mirrors.edge.kernel.org; do
    if apk update \
        --repository "https://$mirror/alpine/v3.21/main" \
        --repository "https://$mirror/alpine/v3.21/community" 2>/dev/null; then
        echo "✅ Using mirror: $mirror" >&2
        break
    fi
done || { echo "❌ All mirrors unreachable"; exit 1; }

ln -s /var/cache/apk /etc/apk/cache 2>/dev/null || true

# Set up target filesystem structure and keys
mkdir -p /target/etc/apk/keys
cp -r /etc/apk/keys/* /target/etc/apk/keys/
echo "https://dl-cdn.alpinelinux.org/alpine/v3.21/main" > /target/etc/apk/repositories
echo "https://dl-cdn.alpinelinux.org/alpine/v3.21/community" >> /target/etc/apk/repositories

# Install to target rootfs
apk add --root /target --no-cache --update-cache --initdb \
    alpine-base bash ca-certificates curl gcc git linux-headers make musl-dev \
    openssl wget \
    go golangci-lint staticcheck

echo "Builder packages installed successfully."
'
}

# ── Build AegisClaw binaries ─────────────────────────────────────────────────
build_binaries() {
    echo ">>> Building guest-agent binary..."
    
    # Locate go binary (sudo resets PATH, check common locations)
    GO_BIN=""
    for candidate in "$(command -v go 2>/dev/null)" /usr/local/go/bin/go /usr/lib/go/bin/go "${GOROOT:-/nonexistent}/bin/go" /snap/go/current/bin/go; do
        if [ -x "${candidate}" ]; then
            GO_BIN="${candidate}"
            break
        fi
    done
    if [ -z "${GO_BIN}" ] && [ -n "${SUDO_USER:-}" ]; then
        # Try the invoking user's go installation
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
        cd "$PROJECT_ROOT"
        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
            "${GO_BIN}" build -buildvcs=false -ldflags="-s -w" -o "${WORKDIR}/guest-agent" ./cmd/guest-agent
    )
    echo "guest-agent binary: $(du -sh "${WORKDIR}/guest-agent" | cut -f1)"
    
    echo ">>> Building builder-agent binary..."
    (
        cd "$PROJECT_ROOT"
        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
            "${GO_BIN}" build -buildvcs=false -ldflags="-s -w" -o "${WORKDIR}/builder-agent" ./cmd/builder-agent
    )
    echo "builder-agent binary: $(du -sh "${WORKDIR}/builder-agent" | cut -f1)"
}

# ── Main pipeline ───────────────────────────────────────────────────────────
main() {
    # 1. Build binaries
    echo "[1/6] Building AegisClaw binaries..."
    build_binaries
    
    # 2. Create / overwrite ext4 image
    echo "[2/6] Creating ext4 image (2048MB)..."
    rm -f "$OUTPUT_IMAGE"
    truncate -s 2048M "$OUTPUT_IMAGE"
    mkfs.ext4 -F -L builder "$OUTPUT_IMAGE"

    # 3. Mount
    echo "[3/6] Mounting image..."
    sudo mount "$OUTPUT_IMAGE" "$MOUNT"

    # 4. Bootstrap Alpine + dev tools
    echo "[4/6] Installing Alpine base and dev tools..."
    setup_builder
    
    # 5. Install binaries
    echo "[5/6] Installing AegisClaw binaries..."
    sudo mkdir -p "$MOUNT/sbin"
    sudo cp "${WORKDIR}/guest-agent" "$MOUNT/sbin/guest-agent"
    sudo cp "${WORKDIR}/builder-agent" "$MOUNT/sbin/builder-agent"
    sudo chmod 755 "$MOUNT/sbin/guest-agent" "$MOUNT/sbin/builder-agent"
    sudo ln -sf /sbin/guest-agent "$MOUNT/init"

    # 6. Finalize structure
    echo "[6/6] Finalizing filesystem..."
    sudo mkdir -p "$MOUNT/workspace" "$MOUNT/go" "$MOUNT/.cache" "$MOUNT/etc"
    sudo chmod 1777 "$MOUNT/workspace"
    echo "aegisclaw-builder" | sudo tee "$MOUNT/etc/hostname" > /dev/null

    # Unmount + resize
    echo ">>> Unmounting and finalizing..."
    sudo umount "$MOUNT"
    sudo resize2fs "$OUTPUT_IMAGE"

    echo "=== Builder rootfs build complete ==="
    echo "Output: $OUTPUT_IMAGE"
    echo "Size:   $(du -sh "$OUTPUT_IMAGE" | cut -f1)"
    echo "Binaries: guest-agent (PID 1), builder-agent (background)"
}

main "$@"
