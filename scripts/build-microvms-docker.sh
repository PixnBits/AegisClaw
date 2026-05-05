#!/bin/bash
set -euo pipefail

# build-microvms-docker.sh — Build microVM rootfs images using Docker templates.
#
# Usage: sudo ./scripts/build-microvms-docker.sh [--target=<target>] [--mode=<mode>] [output-path-prefix]
#
# Targets: guest, aegishub, portal, builder  (default: build all)
# Modes:
#   ext4  (default) — Create ext4 disk images for Firecracker microVMs.
#   image           — Build and tag runnable OCI Docker images (Docker-mode sandbox backend).
# Requirements: root privileges, docker, e2fsprogs (mkfs.ext4, e2fsck, resize2fs)

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/.."
DOCKERFILE_ROOTFS="${PROJECT_ROOT}/Dockerfile.rootfs"
IMAGE_NAME="aegisclaw-rootfs-templates"

# ── Parse arguments ──────────────────────────────────────────────────────────
FILTER_TARGET=""
BUILD_MODE="ext4"
POSITIONAL_ARGS=()
for arg in "$@"; do
    case "$arg" in
        --target=*)
            FILTER_TARGET="${arg#--target=}"
            ;;
        --mode=*)
            BUILD_MODE="${arg#--mode=}"
            ;;
        *)
            POSITIONAL_ARGS+=("$arg")
            ;;
    esac
done

if [[ -n "${FILTER_TARGET}" ]]; then
    case "${FILTER_TARGET}" in
        guest|aegishub|portal|builder) ;;
        *)
            echo "ERROR: Unknown target '${FILTER_TARGET}'. Valid targets: guest, aegishub, portal, builder"
            exit 1
            ;;
    esac
fi

case "${BUILD_MODE}" in
    ext4|image) ;;
    *)
        echo "ERROR: Unknown mode '${BUILD_MODE}'. Valid modes: ext4, image"
        exit 1
        ;;
esac

# Default output prefix
OUTPUT_PREFIX="${POSITIONAL_ARGS[0]:-/var/lib/aegisclaw/rootfs-templates}"

# Ensure we are root
if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: This script must be running as root"
    exit 1
fi

# ── Detect host architecture ──────────────────────────────────────────────────
HOST_ARCH="$(uname -m)"
case "${HOST_ARCH}" in
    x86_64)        GOARCH="amd64" ;;
    aarch64|arm64) GOARCH="arm64" ;;
    *)
        echo "ERROR: Unsupported host architecture: ${HOST_ARCH}"
        exit 1
        ;;
esac

# ── Build Docker Image ───────────────────────────────────────────────────────
echo ">>> Building Docker template image: ${IMAGE_NAME} (platform: linux/${GOARCH})..."
docker build --platform "linux/${GOARCH}" -t "${IMAGE_NAME}" -f "${DOCKERFILE_ROOTFS}" "${PROJECT_ROOT}"

# ─── Image mode: build runnable OCI images for Docker-mode sandbox backend ──
if [[ "${BUILD_MODE}" == "image" ]]; then
    # Fields: name | binary_src_pkg | binary_dest | oci_dockerfile_target | oci_tag
    declare -a image_targets=(
        "guest|./cmd/guest-agent/|/sbin/guest-agent|aegisclaw-guest|aegisclaw/guest:latest"
        "aegishub|./cmd/aegishub/|/sbin/aegishub|aegisclaw-aegishub|aegisclaw/aegishub:latest"
        "portal|./cmd/aegisportal/|/sbin/aegisportal|aegisclaw-portal|aegisclaw/portal:latest"
        "builder|./cmd/builder/|/sbin/builder-agent|aegisclaw-builder|aegisclaw/builder:latest"
    )

    for target_info in "${image_targets[@]}"; do
        IFS='|' read -r TARGET BIN_SRC BIN_DEST DOCKERFILE_TARGET OCI_TAG <<< "${target_info}"

        if [[ -n "${FILTER_TARGET}" && "${TARGET}" != "${FILTER_TARGET}" ]]; then
            continue
        fi

        echo ""
        echo "=== Building OCI image: ${OCI_TAG} (target: ${TARGET}) ==="

        # Build a temporary image with just the binary compiled in.
        TMP_BINARY="$(mktemp -d)/$(basename "${BIN_DEST}")"

        if [ -d "${PROJECT_ROOT}/${BIN_SRC}" ]; then
            echo ">>> Compiling ${BIN_SRC}..."
            (
                cd "${PROJECT_ROOT}"
                export PATH=$PATH:/usr/local/go/bin
                CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH}" \
                    go build -ldflags="-s -w" -o "${TMP_BINARY}" "${BIN_SRC}"
            )
            chmod 755 "${TMP_BINARY}"
        else
            echo "WARNING: Binary source ${BIN_SRC} not found; OCI image will lack the binary."
            TMP_BINARY=""
        fi

        # Build the OCI image from the Dockerfile stage, injecting the binary.
        TMPDIR_CTX="$(mktemp -d)"
        cp "${DOCKERFILE_ROOTFS}" "${TMPDIR_CTX}/Dockerfile"
        if [[ -n "${TMP_BINARY}" ]]; then
            cp "${TMP_BINARY}" "${TMPDIR_CTX}/$(basename "${BIN_DEST}")"
            # Append a COPY that injects the compiled binary.
            printf '\nFROM %s AS final-with-bin\nCOPY %s %s\n' \
                "${DOCKERFILE_TARGET}" "$(basename "${BIN_DEST}")" "${BIN_DEST}" \
                >> "${TMPDIR_CTX}/Dockerfile"
            BUILD_TARGET="final-with-bin"
        else
            BUILD_TARGET="${DOCKERFILE_TARGET}"
        fi

        docker build \
            --platform "linux/${GOARCH}" \
            --target "${BUILD_TARGET}" \
            -t "${OCI_TAG}" \
            -f "${TMPDIR_CTX}/Dockerfile" \
            "${TMPDIR_CTX}"

        rm -rf "${TMPDIR_CTX}" "$(dirname "${TMP_BINARY}")" 2>/dev/null || true
        echo "=== OCI image ${OCI_TAG} built successfully ==="
    done

    echo ""
    echo "=== All requested OCI images built ==="
    exit 0
fi

# Create a temporary container to extract templates from
CONTAINER_ID=$(docker create "${IMAGE_NAME}")

# ── Cleanup handler ───────────────────────────────────────────────────────────
# These variables are updated as resources are acquired so that EXIT cleanup
# can release them even if the script exits mid-loop due to an error.
ACTIVE_MOUNTPOINT=""
ACTIVE_TEMP_EXTRACT_DIR=""

cleanup() {
    echo "Cleaning up..."
    if [[ -n "${ACTIVE_MOUNTPOINT}" ]] && mountpoint -q "${ACTIVE_MOUNTPOINT}" 2>/dev/null; then
        umount "${ACTIVE_MOUNTPOINT}" 2>/dev/null || true
    fi
    [[ -n "${ACTIVE_MOUNTPOINT}" ]] && rm -rf "${ACTIVE_MOUNTPOINT}" 2>/dev/null || true
    [[ -n "${ACTIVE_TEMP_EXTRACT_DIR}" ]] && rm -rf "${ACTIVE_TEMP_EXTRACT_DIR}" 2>/dev/null || true
    docker rm "${CONTAINER_ID}" 2>/dev/null || true
}
trap cleanup EXIT

# ── Setup Targets ─────────────────────────────────────────────────────────────
# Fields: name | binary_src_pkg | binary_dest | ext4_filename | docker_template_path | size_mb
declare -a targets=(
    "guest|./cmd/guest-agent/|/sbin/guest-agent|alpine.ext4|/templates/guest|256"
    "aegishub|./cmd/aegishub/|/sbin/aegishub|aegishub-rootfs.ext4|/templates/aegishub|64"
    "portal|./cmd/aegisportal/|/sbin/aegisportal|portal-rootfs.ext4|/templates/portal|96"
    "builder|./cmd/builder/|/sbin/builder-agent|builder.ext4|/templates/builder|4096"
)

for target_info in "${targets[@]}"; do
    IFS='|' read -r TARGET BIN_SRC BIN_DEST EXT4_NAME TEMP_PATH SIZE_MB <<< "${target_info}"

    # Skip targets not matching the requested filter
    if [[ -n "${FILTER_TARGET}" && "${TARGET}" != "${FILTER_TARGET}" ]]; then
        continue
    fi

    OUTPUT="${OUTPUT_PREFIX}/${EXT4_NAME}"
    echo ""
    echo "=== Building Target: ${TARGET} ==="
    echo "Output: ${OUTPUT}"
    echo "Template: ${TEMP_PATH}"

    # 1. Create ext4 image
    if ! [[ "${SIZE_MB}" =~ ^[1-9][0-9]*$ ]]; then
        echo "ERROR: Invalid SIZE_MB '${SIZE_MB}' for target ${TARGET}"
        exit 1
    fi
    mkdir -p "$(dirname "${OUTPUT}")"
    truncate -s "${SIZE_MB}M" "${OUTPUT}"
    mkfs.ext4 -F -q -L "${TARGET}" "${OUTPUT}"

    # 2. Mount image
    MOUNTPOINT=$(mktemp -d)
    ACTIVE_MOUNTPOINT="${MOUNTPOINT}"
    mount -o loop "${OUTPUT}" "${MOUNTPOINT}"

    # 3. Extract template contents from Docker container.
    # The trailing /. copies the *contents* of the template directory rather
    # than nesting it as a subdirectory inside the destination.
    echo ">>> Extracting template from Docker..."
    TEMP_EXTRACT_DIR=$(mktemp -d)
    ACTIVE_TEMP_EXTRACT_DIR="${TEMP_EXTRACT_DIR}"
    docker cp "${CONTAINER_ID}:${TEMP_PATH}/." "${TEMP_EXTRACT_DIR}"

    # 4. Populate rootfs with template contents
    echo ">>> Populating rootfs..."
    cp -a "${TEMP_EXTRACT_DIR}/." "${MOUNTPOINT}/"
    mkdir -p "${MOUNTPOINT}/workspace"

    # 5. Build and inject the target binary directly into the rootfs
    if [ -d "${PROJECT_ROOT}/${BIN_SRC}" ]; then
        echo ">>> Building binary: ${BIN_SRC} -> ${BIN_DEST}"
        (
            cd "${PROJECT_ROOT}"
            export PATH=$PATH:/usr/local/go/bin
            CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH}" \
                go build -ldflags="-s -w" -o "${MOUNTPOINT}${BIN_DEST}" "${BIN_SRC}"
        )
        chmod 755 "${MOUNTPOINT}${BIN_DEST}"
    else
        echo "WARNING: Binary source ${BIN_SRC} not found. Skipping binary build."
    fi

    # 6. Finalize filesystem
    echo ">>> Finalizing filesystem..."
    umount "${MOUNTPOINT}"
    ACTIVE_MOUNTPOINT=""
    e2fsck -f -y "${OUTPUT}" || true
    resize2fs -M "${OUTPUT}"

    # 7. Clean up per-target temporaries
    rm -rf "${MOUNTPOINT}" "${TEMP_EXTRACT_DIR}"
    ACTIVE_TEMP_EXTRACT_DIR=""

    echo "=== Target: ${TARGET} complete ==="
done

echo ""
echo "=== All requested microVM rootfs builds complete ==="
