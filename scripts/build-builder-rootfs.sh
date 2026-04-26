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
MOUNT="$(mktemp -d -t aegisclaw-builder-XXXXXX)"
trap 'sudo umount "$MOUNT" 2>/dev/null || true; sudo rm -rf "$MOUNT"' EXIT

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

apk add --no-cache --update-cache \
    alpine-base bash ca-certificates curl gcc git linux-headers make musl-dev \
    openssl wget \
    go golangci-lint staticcheck

go env -w GOMODCACHE=/root/go/pkg/mod
echo "Builder packages installed successfully."
'
}

# ── Main pipeline ───────────────────────────────────────────────────────────
main() {
    # 1. Create / overwrite ext4 image
    echo "[1/5] Creating ext4 image (2048MB)..."
    rm -f "$OUTPUT_IMAGE"
    truncate -s 2048M "$OUTPUT_IMAGE"
    mkfs.ext4 -F -L builder "$OUTPUT_IMAGE"

    # 2. Mount
    echo "[2/5] Mounting image..."
    sudo mount "$OUTPUT_IMAGE" "$MOUNT"

    # 3. Bootstrap
    setup_builder

    # 4. Finalize structure
    echo "[4/5] Finalizing filesystem..."
    sudo mkdir -p "$MOUNT/workspace" "$MOUNT/go" "$MOUNT/.cache"
    sudo chmod 1777 "$MOUNT/workspace"

    # 5. Unmount + resize
    echo "[5/5] Finalizing image..."
    sudo umount "$MOUNT"
    sudo resize2fs "$OUTPUT_IMAGE"

    echo "=== Builder rootfs build complete ==="
    echo "Output: $OUTPUT_IMAGE"
    echo "Size:   $(du -sh "$OUTPUT_IMAGE" | cut -f1)"
    echo "Next:   sudo make build-rootfs   (to include in full build)"
}

main "$@"
