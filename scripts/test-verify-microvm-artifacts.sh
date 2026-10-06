#!/bin/bash
# Offline checks for scripts/verify-microvm-artifacts.sh. Fixtures are built
# in a temp directory with no root and no docker daemon.
#
# The good image is 4M on purpose. That size has no journal: file(1) reports
# ext2, blkid reports ext4. Exit 0 on that fixture means the ext4 probe used
# blkid. The file(1) fallback is covered separately with an 8M image and a
# PATH that does not include blkid.
#
# Run: bash scripts/test-verify-microvm-artifacts.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERIFY="$SCRIPT_DIR/verify-microvm-artifacts.sh"

if [ ! -f "$VERIFY" ]; then
    echo "FAIL: verifier not found at $VERIFY" >&2
    exit 1
fi
if ! command -v mkfs.ext4 >/dev/null 2>&1; then
    echo "FAIL: mkfs.ext4 is required (e2fsprogs)" >&2
    exit 1
fi
if ! command -v blkid >/dev/null 2>&1; then
    echo "FAIL: blkid is required for the primary ext4 cases" >&2
    exit 1
fi

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT
ROOT="$WORKDIR/rootfs"
mkdir -p "$ROOT"

CAPTURE=""
RC=0

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

make_ext4() {
    local dest="$1"
    local size="${2:-4M}"
    rm -f -- "$dest"
    truncate -s "$size" "$dest"
    if ! mkfs.ext4 -q -F -L rootfs "$dest" >/dev/null 2>&1; then
        fail "mkfs.ext4 failed for $dest ($size)"
    fi
}

make_tar() {
    local dest="$1"
    local work
    work=$(mktemp -d)
    mkdir -p "$work/bin" "$work/etc"
    printf 'x\n' > "$work/bin/sh"
    tar -czf "$dest" -C "$work" .
    rm -rf "$work"
}

# run_verify [env KEY=VAL ...] -- [verifier args...]
run_verify() {
    local -a env_args=()
    local -a args=()
    local seen=0
    local arg
    for arg in "$@"; do
        if [ "$seen" -eq 0 ] && [ "$arg" = "--" ]; then
            seen=1
            continue
        fi
        if [ "$seen" -eq 0 ]; then
            env_args+=("$arg")
        else
            args+=("$arg")
        fi
    done
    if [ "$seen" -ne 1 ]; then
        fail "run_verify: missing --"
    fi
    set +e
    CAPTURE=$(
        env -u AEGIS_KERNEL_PATH -u VERIFY_SKIP_DOCKER \
            "${env_args[@]}" \
            /usr/bin/bash "$VERIFY" "${args[@]}" 2>&1
    )
    RC=$?
    set -e
}

assert_rc() {
    local want="$1"
    local why="$2"
    if [ "$RC" -ne "$want" ]; then
        printf '%s\n' "$CAPTURE" >&2
        fail "$why (rc=$RC, want=$want)"
    fi
}

assert_contains() {
    local needle="$1"
    local why="$2"
    if ! printf '%s\n' "$CAPTURE" | grep -F -q -- "$needle"; then
        printf '%s\n' "$CAPTURE" >&2
        fail "$why (missing: $needle)"
    fi
}

assert_not_contains() {
    local needle="$1"
    local why="$2"
    if printf '%s\n' "$CAPTURE" | grep -F -q -- "$needle"; then
        printf '%s\n' "$CAPTURE" >&2
        fail "$why (unexpected: $needle)"
    fi
}

make_ext4 "$ROOT/good.img" 4M
make_tar "$ROOT/good.img.tar.gz"

truncate -s 4M "$ROOT/zero.img"
make_tar "$ROOT/zero.img.tar.gz"

make_ext4 "$ROOT/trunc.img" 4M
make_tar "$ROOT/trunc.img.tar.gz"
dd if="$ROOT/trunc.img.tar.gz" of="$ROOT/trunc.img.tar.gz.part" bs=16 count=1 status=none
mv -f "$ROOT/trunc.img.tar.gz.part" "$ROOT/trunc.img.tar.gz"

make_ext4 "$ROOT/nottar.img" 4M
printf 'not a tar\n' | gzip -c > "$ROOT/nottar.img.tar.gz"

make_ext4 "$ROOT/fallback.img" 8M
make_tar "$ROOT/fallback.img.tar.gz"

echo "1. good ext4 rootfs and tar pass with VERIFY_SKIP_DOCKER=1"
run_verify VERIFY_SKIP_DOCKER=1 -- "$ROOT" good
assert_rc 0 "good fixtures should pass"
assert_not_contains "FAIL:" "good fixtures reported a failure"

echo "2. --skip-docker is enough when VERIFY_SKIP_DOCKER is unset"
run_verify -- --skip-docker "$ROOT" good
assert_rc 0 "--skip-docker should pass the good fixtures"
assert_not_contains "FAIL:" "--skip-docker reported a failure"
assert_not_contains "aegis-good:latest" "--skip-docker still checked the docker image"

echo "3. a non-empty image that is not ext4 fails and names the component"
run_verify VERIFY_SKIP_DOCKER=1 -- "$ROOT" zero
assert_rc 1 "zero-filled image should fail"
assert_contains "FAIL: zero:" "failure did not name zero"
assert_contains "not an ext4 filesystem" "ext4 probe did not reject the zero image"

echo "4. truncated gzip fails gzip -t and names the component"
run_verify VERIFY_SKIP_DOCKER=1 -- "$ROOT" trunc
assert_rc 1 "truncated tarball should fail"
assert_contains "FAIL: trunc:" "failure did not name trunc"
assert_contains "failed gzip -t" "gzip -t failure was not reported"

echo "5. a gzip that is not a tar fails tar -tzf and names the component"
run_verify VERIFY_SKIP_DOCKER=1 -- "$ROOT" nottar
assert_rc 1 "non-tar gzip should fail"
assert_contains "FAIL: nottar:" "failure did not name nottar"
assert_contains "failed tar -tzf" "tar -tzf failure was not reported"
assert_not_contains "failed gzip -t" "non-tar gzip was blamed on gzip -t"

echo "6. missing artifacts fail and name the component"
run_verify VERIFY_SKIP_DOCKER=1 -- "$ROOT" gone
assert_rc 1 "missing files should fail"
assert_contains "FAIL: gone:" "failure did not name gone"
assert_contains "missing ${ROOT}/gone.img" "missing image was not reported"
assert_contains "missing ${ROOT}/gone.img.tar.gz" "missing tarball was not reported"

echo "7. failures are collected across components"
run_verify VERIFY_SKIP_DOCKER=1 -- "$ROOT" zero trunc
assert_rc 1 "combined bad fixtures should fail"
assert_contains "FAIL: zero:" "collected output dropped zero"
assert_contains "not an ext4 filesystem" "collected output dropped the ext4 failure"
assert_contains "FAIL: trunc:" "collected output dropped trunc"
assert_contains "failed gzip -t" "collected output dropped the gzip failure"

echo "8. without a skip flag, a missing docker image fails the good component"
run_verify -- "$ROOT" good
assert_rc 1 "missing docker image should fail"
assert_contains "missing docker image aegis-good:latest" "docker image was not required"

echo "9. --kernel rejects a missing or empty file and accepts a non-empty one"
run_verify VERIFY_SKIP_DOCKER=1 -- --kernel "$ROOT/no-such-vmlinux" "$ROOT" good
assert_rc 1 "missing kernel should fail"
assert_contains "kernel: ${ROOT}/no-such-vmlinux is missing or empty" "missing kernel was not reported"
: > "$ROOT/empty-vmlinux"
run_verify VERIFY_SKIP_DOCKER=1 -- --kernel "$ROOT/empty-vmlinux" "$ROOT" good
assert_rc 1 "empty kernel should fail"
assert_contains "kernel: ${ROOT}/empty-vmlinux is missing or empty" "empty kernel was not reported"
printf 'virtio_rng\n' > "$ROOT/vmlinux"
run_verify VERIFY_SKIP_DOCKER=1 -- --kernel "$ROOT/vmlinux" "$ROOT" good
assert_rc 0 "non-empty kernel plus good rootfs should pass"
assert_not_contains "FAIL:" "kernel pass reported a failure"

echo "10. file(1) fallback accepts a journaled ext4 image when blkid is absent"
run_verify VERIFY_SKIP_DOCKER=1 PATH="/usr/bin:/bin" -- "$ROOT" fallback
assert_rc 0 "file fallback should accept the 8M ext4 image"
assert_not_contains "FAIL:" "file fallback reported a failure"
run_verify VERIFY_SKIP_DOCKER=1 PATH="/usr/bin:/bin" -- "$ROOT" zero
assert_rc 1 "file fallback should reject a zero image"
assert_contains "FAIL: zero:" "file fallback did not name zero"
assert_contains "not an ext4 filesystem" "file fallback did not reject the zero image"

echo "11. missing blkid and file is a named failure, not a crash"
TOOLBIN="$WORKDIR/tools"
mkdir -p "$TOOLBIN"
ln -s /usr/bin/gzip "$TOOLBIN/gzip"
ln -s /usr/bin/tar "$TOOLBIN/tar"
ln -s /usr/bin/grep "$TOOLBIN/grep"
ln -s /usr/bin/dirname "$TOOLBIN/dirname"
ln -s /usr/bin/pwd "$TOOLBIN/pwd"
run_verify VERIFY_SKIP_DOCKER=1 PATH="$TOOLBIN" -- "$ROOT" good
assert_rc 1 "missing filesystem tools should fail"
assert_contains "FAIL: good:" "missing-tool failure did not name good"
assert_contains "neither blkid nor file is available" "missing tools were not explained"

echo "12. no component arguments uses --print-default-components"
mkdir -p "$WORKDIR/empty-rootfs"
run_verify VERIFY_SKIP_DOCKER=1 -- "$WORKDIR/empty-rootfs"
assert_rc 1 "empty rootfs with the default list should fail"
assert_contains "FAIL: agent:" "default list did not include agent"
assert_contains "FAIL: court-scribe:" "default list did not include court-scribe"

echo "ok: verify-microvm-artifacts.sh format checks"
