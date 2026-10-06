#!/bin/bash
# Proves scripts/build-microvms-docker.sh treats positional arguments as
# component names and does not mask a missing Dockerfile, without sudo,
# docker, loop mounts, or the guest-kernel download.
#
# Mutant: a missing Dockerfile used to `warn` and `continue`, so invoking the
# script with an output directory as $1 (the CI bug) exited 0. Restoring that
# continue makes the bogus-component case exit 0. This file runs that mutant
# on a copy of the script and requires the real script to exit non-zero.
#
# Run: bash scripts/test-build-microvms-args.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_SCRIPT="$SCRIPT_DIR/build-microvms-docker.sh"

if [ ! -f "$BUILD_SCRIPT" ]; then
    echo "FAIL: build script not found at $BUILD_SCRIPT" >&2
    exit 1
fi

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

FAKEBIN="$WORKDIR/bin"
OUT="$WORKDIR/out"
MARKER="$WORKDIR/marker"
mkdir -p "$FAKEBIN" "$WORKDIR/home"
: > "$MARKER"

# Absolute bash starts the script under test. A PATH entry named bash records
# and rejects the kernel-download invocation if the script reaches it.
cat > "$FAKEBIN/bash" << 'EOF'
#!/bin/sh
if [ -n "${MARKER_FILE:-}" ]; then
    printf 'bash %s\n' "$*" >> "$MARKER_FILE"
fi
echo "fake bash: refusing $*" >&2
exit 1
EOF
cat > "$FAKEBIN/docker" << 'EOF'
#!/bin/sh
if [ -n "${MARKER_FILE:-}" ]; then
    printf 'docker %s\n' "$*" >> "$MARKER_FILE"
fi
echo "fake docker: refusing $*" >&2
exit 1
EOF
cat > "$FAKEBIN/sudo" << 'EOF'
#!/bin/sh
if [ -n "${MARKER_FILE:-}" ]; then
    printf 'sudo %s\n' "$*" >> "$MARKER_FILE"
fi
echo "fake sudo: refusing $*" >&2
exit 1
EOF
chmod 755 "$FAKEBIN/bash" "$FAKEBIN/docker" "$FAKEBIN/sudo"

BASE_PATH="$PATH"
CAPTURE=""
RC=0

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

# run_script [KEY=VAL...] -- [component args...]
run_script() {
    local env_args=()
    local script_args=()
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
            script_args+=("$arg")
        fi
    done
    if [ "$seen" -ne 1 ]; then
        fail "run_script: missing --"
    fi
    : > "$MARKER"
    rm -rf "$OUT"
    set +e
    if [ "${#script_args[@]}" -eq 0 ]; then
        CAPTURE=$(
            env -u AEGIS_DRY_RUN -u AEGIS_SKIP_KERNEL_ENSURE \
                PATH="$FAKEBIN:$BASE_PATH" \
                MARKER_FILE="$MARKER" \
                HOME="$WORKDIR/home" \
                ROOTFS_DIR="$OUT" \
                "${env_args[@]}" \
                /usr/bin/bash "$BUILD_SCRIPT" 2>&1
        )
    else
        CAPTURE=$(
            env -u AEGIS_DRY_RUN -u AEGIS_SKIP_KERNEL_ENSURE \
                PATH="$FAKEBIN:$BASE_PATH" \
                MARKER_FILE="$MARKER" \
                HOME="$WORKDIR/home" \
                ROOTFS_DIR="$OUT" \
                "${env_args[@]}" \
                /usr/bin/bash "$BUILD_SCRIPT" "${script_args[@]}" 2>&1
        )
    fi
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

assert_marker_empty() {
    if [ -s "$MARKER" ]; then
        echo "marker:" >&2
        cat "$MARKER" >&2
        printf '%s\n' "$CAPTURE" >&2
        fail "docker, sudo, or the kernel downloader was invoked"
    fi
}

assert_marker_has() {
    local needle="$1"
    local why="$2"
    if ! grep -F -q -- "$needle" "$MARKER"; then
        echo "marker:" >&2
        cat "$MARKER" >&2
        fail "$why (marker missing: $needle)"
    fi
}

assert_marker_lacks() {
    local needle="$1"
    local why="$2"
    if grep -F -q -- "$needle" "$MARKER"; then
        echo "marker:" >&2
        cat "$MARKER" >&2
        fail "$why (marker has: $needle)"
    fi
}

assert_out_absent() {
    if [ -e "$OUT" ]; then
        fail "ROOTFS_DIR was created at $OUT"
    fi
}

DEFAULT_COMPONENTS=(
    agent
    project-manager
    web-portal
    builder
    store
    memory
    network-boundary
    court-persona
    court-scribe
)

echo "1. default component list is validated and nothing is built"
run_script AEGIS_DRY_RUN=1 --
assert_rc 0 "default dry run should succeed"
assert_contains "Output directory: $OUT" "ROOTFS_DIR was not honored"
assert_contains "DRY_RUN: would run scripts/download-firecracker-kernel.sh" "dry run should plan the kernel ensure without running it"
for c in "${DEFAULT_COMPONENTS[@]}"; do
    assert_contains "DRY_RUN: would build image aegis-${c}:latest" "default list missing $c"
    assert_contains "DRY_RUN: would write $OUT/${c}.img and $OUT/${c}.img.tar.gz" "default output path missing $c"
done
assert_marker_empty
assert_out_absent

echo "2. an output directory passed as the component list is a hard failure"
run_script AEGIS_DRY_RUN=1 -- /tmp/rootfs-templates
assert_rc 1 "bogus component should fail (continue would mask this)"
assert_contains "Dockerfile not found for requested component /tmp/rootfs-templates" "missing Dockerfile was not reported"
assert_contains "Building microVM filesystems for: /tmp/rootfs-templates" "positional arg was not used as the component list"
assert_contains "Output directory: $OUT" "positional arg was treated as the output directory"
assert_marker_empty
assert_out_absent

echo "3. AEGIS_SKIP_KERNEL_ENSURE=1 warns and does not download"
run_script AEGIS_DRY_RUN=1 AEGIS_SKIP_KERNEL_ENSURE=1 --
assert_rc 0 "skip + dry run should succeed"
assert_contains "AEGIS_SKIP_KERNEL_ENSURE=1: NOT ensuring the Firecracker guest kernel" "skip opt-out was not announced"
assert_not_contains "DRY_RUN: would run scripts/download-firecracker-kernel.sh" "skip opt-out still planned a kernel download"
assert_marker_empty
assert_out_absent

echo "4. explicit component arguments are the list, not the first word only"
run_script AEGIS_DRY_RUN=1 -- agent store
assert_rc 0 "multi-arg dry run should succeed"
assert_contains "DRY_RUN: would build image aegis-agent:latest" "agent was not selected"
assert_contains "DRY_RUN: would build image aegis-store:latest" "store was not selected"
assert_not_contains "aegis-project-manager:latest" "unrequested component was selected"
assert_marker_empty
assert_out_absent

echo "5. one space-separated argument is still a component list"
run_script AEGIS_DRY_RUN=1 -- "memory court-scribe"
assert_rc 0 "single space-separated argument should succeed"
assert_contains "DRY_RUN: would build image aegis-memory:latest" "memory was not selected from the combined argument"
assert_contains "DRY_RUN: would build image aegis-court-scribe:latest" "court-scribe was not selected from the combined argument"
assert_not_contains "aegis-builder:latest" "unrequested component was selected from the combined argument"
assert_marker_empty

if [ "$(uname -s)" = "Linux" ]; then
    echo "6. kernel ensure failure is fatal and stops before docker or sudo"
    run_script --
    assert_rc 1 "kernel ensure failure should fail the build"
    assert_contains "Kernel download/ensure failed" "kernel failure was not fatal"
    assert_marker_has "bash " "kernel downloader was not invoked"
    assert_marker_lacks "docker " "docker ran after a kernel failure"
    assert_marker_lacks "sudo " "sudo ran after a kernel failure"
    assert_out_absent
else
    echo "6. skip kernel-fatal case (kernel ensure runs only on Linux)"
fi

echo "7. mutant: warn+continue on a missing Dockerfile exits 0"
MUTANT="$WORKDIR/mutant.sh"
awk '
    /error "Dockerfile not found for requested component/ {
        print "        warn \"Dockerfile not found, skipping\""
        print "        continue"
        replaced = 1
        next
    }
    { print }
    END {
        if (!replaced) {
            print "mutant rewrite did not find the fatal Dockerfile check" > "/dev/stderr"
            exit 2
        }
    }
' "$BUILD_SCRIPT" > "$MUTANT"
# Point the runner at the mutant for this case only.
REAL_BUILD_SCRIPT="$BUILD_SCRIPT"
BUILD_SCRIPT="$MUTANT"
run_script AEGIS_DRY_RUN=1 -- /tmp/rootfs-templates
BUILD_SCRIPT="$REAL_BUILD_SCRIPT"
assert_rc 0 "mutant with continue should mask the missing Dockerfile"
assert_contains "Dockerfile not found, skipping" "mutant did not take the continue path"
assert_not_contains "[ERROR]" "mutant still reported a fatal error"
assert_marker_empty
assert_out_absent

echo "ok: build-microvms-docker.sh argument and failure guards"
