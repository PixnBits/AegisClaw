#!/bin/bash
# verify-microvm-artifacts.sh — check microVM build outputs.
#
# Usage:
#   scripts/verify-microvm-artifacts.sh [--skip-docker] [--kernel PATH] <rootfs_dir> [component ...]
#
# With no component names, the list is the stdout of
# scripts/build-microvms-docker.sh --print-default-components.
#
# Per component:
#   <dir>/<c>.img          ext4. Probe with `blkid -p -o value -s TYPE`.
#                          If blkid is not installed, `file -b` must contain
#                          "ext4 filesystem". If neither tool exists, fail.
#   <dir>/<c>.img.tar.gz   passes `gzip -t`, and `tar -tzf` lists "./".
#                          build-microvms-docker.sh archives the rootfs with
#                          `tar -C <component-rootfs> .`, which GNU tar records
#                          as that member.
#   aegis-<c>:latest       `docker image inspect` succeeds, unless
#                          VERIFY_SKIP_DOCKER=1 or --skip-docker.
#
# --kernel PATH, or AEGIS_KERNEL_PATH when --kernel is omitted, must name a
# non-empty file (the path the image job passes to the kernel download) whose
# SHA-256 matches the pin in download-firecracker-kernel.sh
# (--print-pinned-sha256). The same overrides as the download apply:
# AEGIS_KERNEL_SHA256 replaces the pin for a deliberate custom kernel, and
# AEGIS_SKIP_KERNEL_CHECKSUM=1 skips the hash with a WARN line. With no
# sha256sum or shasum, the check fails. With neither --kernel nor
# AEGIS_KERNEL_PATH, the kernel is not checked.
#
# Every failure is printed. The script then exits 1. It does not stop at the
# first one.
#
# GNU tar 1.35 exits 0 and prints no names for a gzip that is not a tar, so
# an empty listing is a tar -tzf failure. file(1) reports a journal-less
# image (under about 8M) as ext2; blkid still reports ext4. Guest images are
# 512M or larger and match both.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

failures=()

note_fail() {
    printf 'FAIL: %s\n' "$*" >&2
    failures+=("$*")
}

verify_ext4() {
    local component="$1"
    local img="$2"
    local fstype="" desc=""

    if command -v blkid >/dev/null 2>&1; then
        fstype=$(blkid -p -o value -s TYPE -- "$img" 2>/dev/null || true)
        if [ "$fstype" != "ext4" ]; then
            note_fail "${component}: ${img} is not an ext4 filesystem (blkid TYPE='${fstype}')"
        fi
        return 0
    fi
    if command -v file >/dev/null 2>&1; then
        desc=$(file -b -- "$img" 2>/dev/null || true)
        case "$desc" in
            *"ext4 filesystem"*)
                ;;
            *)
                note_fail "${component}: ${img} is not an ext4 filesystem (file: ${desc})"
                ;;
        esac
        return 0
    fi
    note_fail "${component}: neither blkid nor file is available to verify ${img} is ext4"
    return 0
}

verify_tarball() {
    local component="$1"
    local tarball="$2"
    local listing="" member=""
    local entries=0 has_root=0

    if [ ! -f "$tarball" ]; then
        note_fail "${component}: missing ${tarball}"
        return 0
    fi

    if ! command -v gzip >/dev/null 2>&1; then
        note_fail "${component}: gzip is not available to test ${tarball}"
    elif ! gzip -t -- "$tarball" 2>/dev/null; then
        note_fail "${component}: ${tarball} failed gzip -t"
    fi

    if ! command -v tar >/dev/null 2>&1; then
        note_fail "${component}: tar is not available to test ${tarball}"
        return 0
    fi
    if ! listing=$(tar -tzf "$tarball" 2>/dev/null); then
        note_fail "${component}: ${tarball} failed tar -tzf"
        return 0
    fi
    while IFS= read -r member || [ -n "$member" ]; do
        [ -z "$member" ] && continue
        entries=$((entries + 1))
        if [ "$member" = "./" ]; then
            has_root=1
        fi
    done <<< "$listing"
    if [ "$entries" -lt 1 ]; then
        note_fail "${component}: ${tarball} failed tar -tzf (no entries listed)"
    elif [ "$has_root" -ne 1 ]; then
        note_fail "${component}: ${tarball} failed tar -tzf (missing rootfs member './')"
    fi
    return 0
}

verify_image() {
    local component="$1"
    local ref="aegis-${component}:latest"

    if ! command -v docker >/dev/null 2>&1; then
        note_fail "${component}: missing docker image ${ref} (docker not available)"
        return 0
    fi
    if ! docker image inspect "$ref" >/dev/null 2>&1; then
        note_fail "${component}: missing docker image ${ref}"
    fi
    return 0
}

# Prints the file's lowercase SHA-256. Returns 2 when no hash tool exists.
kernel_file_sha256() {
    local file="$1" sum=""
    if command -v sha256sum >/dev/null 2>&1; then
        sum=$(sha256sum -- "$file" 2>/dev/null) || return 1
    elif command -v shasum >/dev/null 2>&1; then
        sum=$(shasum -a 256 -- "$file" 2>/dev/null) || return 1
    else
        return 2
    fi
    sum=${sum%% *}
    sum=${sum,,}
    [[ "$sum" =~ ^[0-9a-f]{64}$ ]] || return 1
    printf '%s\n' "$sum"
}

verify_kernel() {
    local path="$1"
    local expected="" source="" actual="" rc=0

    if [ ! -s "$path" ]; then
        note_fail "kernel: ${path} is missing or empty"
        return 0
    fi
    if [ "${AEGIS_SKIP_KERNEL_CHECKSUM:-}" = "1" ]; then
        printf 'WARN: kernel: AEGIS_SKIP_KERNEL_CHECKSUM=1, %s was not hash-checked\n' "$path" >&2
        return 0
    fi
    if [ -n "${AEGIS_KERNEL_SHA256:-}" ]; then
        expected=$AEGIS_KERNEL_SHA256
        source="AEGIS_KERNEL_SHA256"
    else
        source="the pin in download-firecracker-kernel.sh"
        if ! expected=$("$SCRIPT_DIR/download-firecracker-kernel.sh" --print-pinned-sha256); then
            note_fail "kernel: could not read ${source}"
            return 0
        fi
    fi
    expected=${expected,,}
    if [[ ! "$expected" =~ ^[0-9a-f]{64}$ ]]; then
        note_fail "kernel: ${source} is not a SHA-256: '${expected}'"
        return 0
    fi
    actual=$(kernel_file_sha256 "$path") || rc=$?
    if [ "$rc" -eq 2 ]; then
        note_fail "kernel: neither sha256sum nor shasum is available to check ${path}"
        return 0
    elif [ "$rc" -ne 0 ]; then
        note_fail "kernel: could not hash ${path}"
        return 0
    fi
    if [ "$actual" != "$expected" ]; then
        note_fail "kernel: ${path} SHA-256 ${actual} does not match ${source} (${expected})"
    fi
    return 0
}

skip_docker=0
kernel_path=""
check_kernel=0
if [ "${VERIFY_SKIP_DOCKER:-}" = "1" ]; then
    skip_docker=1
fi

positionals=()
while [ "$#" -gt 0 ]; do
    case "$1" in
        --skip-docker)
            skip_docker=1
            shift
            ;;
        --kernel)
            if [ "$#" -lt 2 ] || [ -z "${2:-}" ]; then
                echo "error: --kernel requires a path" >&2
                exit 2
            fi
            kernel_path=$2
            check_kernel=1
            shift 2
            ;;
        --)
            shift
            positionals+=("$@")
            break
            ;;
        -*)
            echo "error: unknown option: $1" >&2
            exit 2
            ;;
        *)
            positionals+=("$1")
            shift
            ;;
    esac
done

if [ "$check_kernel" -eq 0 ] && [ -n "${AEGIS_KERNEL_PATH:-}" ]; then
    kernel_path=$AEGIS_KERNEL_PATH
    check_kernel=1
fi

if [ "${#positionals[@]}" -lt 1 ]; then
    echo "usage: scripts/verify-microvm-artifacts.sh [--skip-docker] [--kernel PATH] <rootfs_dir> [component ...]" >&2
    exit 2
fi

rootfs=${positionals[0]}
components=()
if [ "${#positionals[@]}" -gt 1 ]; then
    components=("${positionals[@]:1}")
fi

if [ "${#components[@]}" -eq 0 ]; then
    raw=""
    build_script="$SCRIPT_DIR/build-microvms-docker.sh"
    if ! raw=$("$build_script" --print-default-components); then
        note_fail "could not read default components from ${build_script}"
    elif [ -z "$raw" ]; then
        note_fail "default component list is empty"
    else
        read -r -a components <<< "$raw"
    fi
fi

if [ "$check_kernel" -eq 1 ]; then
    verify_kernel "$kernel_path"
fi

valid_name='^[A-Za-z0-9][A-Za-z0-9._-]*$'
for component in "${components[@]}"; do
    if [[ ! "$component" =~ $valid_name ]]; then
        note_fail "invalid component name: ${component}"
        continue
    fi
    img="${rootfs}/${component}.img"
    tarball="${img}.tar.gz"
    if [ ! -f "$img" ]; then
        note_fail "${component}: missing ${img}"
    else
        verify_ext4 "$component" "$img"
    fi
    verify_tarball "$component" "$tarball"
    if [ "$skip_docker" -eq 0 ]; then
        verify_image "$component"
    fi
done

if [ "${#components[@]}" -eq 0 ] && [ "${#failures[@]}" -eq 0 ]; then
    note_fail "no components to verify"
fi

if [ "${#failures[@]}" -ne 0 ]; then
    exit 1
fi
echo "ok: verified ${#components[@]} component(s) under ${rootfs}"
exit 0
