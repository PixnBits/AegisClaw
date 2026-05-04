#!/bin/bash
set -euo pipefail

# build-microvms-docker.sh — Build microVM rootfs images using Docker templates.
#
# Usage: sudo ./scripts/build-microvms-docker.sh [output-path-prefix]
#
# Targets: guest, aegishub, portal, builder

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/.."
DOCKERFILE_ROOTFS="${PROJECT_ROOT}/Dockerfile.rootfs"
IMAGE_NAME="aegisclaw-rootfs-templates"

# Default output prefix
OUTPUT_PREFIX="${1:-/var/lib/aegisclaw/rootfs-templates}"

# Ensure we are root
if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: This script must be running as root"
    exit 1
fi

# ── Build Docker Image ───────────────
echo ">>> Building Docker template image: ${IMAGE_NAME}..."
docker build -t "${IMAGE_NAME}" -f "${DOCKERFILE_ROOTFS}" "${PROJECT_ROOT}"

# Create a temporary container to extract templates from
CONTAINER_ID=$(docker create "${IMAGE_NAME}")

# ── Setup Targets ─────────────────────
# Each target: name | binary_src | binary_dest | ext4_name | template_path
declare -a targets=(
    "guest|./cmd/guest-agent/|/sbin/guest-agent|alpine.ext4|/templates/guest"
    "aegishub|./cmd/aegishub/|/sbin/aegishub|aegishub-rootfs.ext4|/templates/aegishub"
    "portal|./cmd/aegisportal/|/sbin/aegisportal|portal-rootfs.ext4|/templates/portal"
    "builder|./cmd/builder/|/sbin/builder-agent|builder.ext4|/templates/builder"
)

cleanup() {
    echo "Cleaning up..."
    docker rm "${CONTAINER_ID}" 2>/dev/null || true
}
trap cleanup EXIT

for target_info in "${targets[@]}"; do
    IFS='|' read -r TARGET BIN_SRC BIN_DEST EXT4_NAME TEMP_PATH <<< "${target_info}"
    
    OUTPUT="${OUTPUT_PREFIX}/${EXT4_NAME}"
    echo ""
    echo "=== Building Target: ${TARGET} ==="
    echo "Output: ${OUTPUT}"
    echo "Template: ${TEMP_PATH}"
    # Wait, I had a typo in the previous version: PAN_PATH. Fixed it below.
    # Actually, I should use the variable I just read.
    
    # Wait, I'll use the variable from the read command: TEMP_PATH
    
    # 1. Create ext4 image
    SIZE_MB=1024
    if [[ "${TARGET}" == "aegishub" || "${TARGET}" == "portal" ]]; then
        SIZE_MB=1024
    fi
    if [[ "${TARGET}" == "builder" ]]; then
        SIZE_MB=4096
    fi

    mkdir -p "$(dirname "${OUTPUT}")"
    
    # Ensure we start with a fresh file of enough size
    truncate -s ${SIZE_MB}M "${OUTPUT}"
    mkfs.ext4 -F -q -L "${TARGET}" "${OUTPUT}"

    # 2. Mount image
    MOUNTPOINT=$(mktemp -d)
    mount -o loop "${OUTPUT}" "${MOUNTPOINT}"

    # 3. Extract template from Docker container
    echo ">>> Extracting templates from Docker..."
    TEMP_EXTRACT_DIR=$(mktemp -d)
    docker cp "${CONTAINER_ID}:${TEMP_PATH}" "${TEMP_EXTRACT_DIR}"

    # 4. Copy extracted files to the mounted rootfs
    echo ">>> Copying template files to rootfs..."
    cp -a "${TEMP_EXTRACT_DIR}/." "${MOUNTPOINT}/"
    mkdir -p "${MOUNTPOINT}/workspace"
    chmod 777 "${MOUNTPOINT}/workspace"
    mkdir -p "${MOUNTPOINT}/workspace"

    # 5. Copy the freshly built host-side binary
    if [ -d "${PROJECT_ROOT}/${BIN_SRC}" ]; then
        echo ">>> Copy and building binary: ${BIN_SRC} -> ${BIN_DEST}"
        (
            cd "${PROJECT_ROOT}"
            export PATH=$PATH:/usr/local/go/bin
            CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
                go build -ldflags="-s -w" -o "${MOUNTPOINT}${BIN_DEST}" "${BIN_SRC}"
        )
        chmod 755 "${MOUNTPOINT}${BIN_DEST}"
    else
        echo "WARNING: Binary source ${BIN_SRC} not found. Skipping binary copy."
    fi

    # 6. Finalize filesystem
    echo ">>> Finalizing filesystem..."
    umount "${MOUNTPOINT}"
    e2fsck -f -y "${OUTPUT}" || true
    resize2fs -M "${OUTPUT}"

    # 7. Cleanup
    rm -rf "${MOUNTPOINT}" "${TEMP_EXTRACT_DIR}"
    
    echo "=== Target: ${TARGET} complete ==="
done

echo ""
echo "=== All microVM rootfs builds complete ==="
