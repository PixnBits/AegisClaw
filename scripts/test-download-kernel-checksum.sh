#!/bin/bash
# Proves scripts/download-firecracker-kernel.sh checksum behavior without the
# network and without touching the real home directory:
#   (a) a mismatched download is rejected and not installed
#   (b) a download whose SHA-256 matches is installed
#   (c) AEGIS_SKIP_KERNEL_CHECKSUM=1 installs anyway, with a loud warning
#
# Run: bash scripts/test-download-kernel-checksum.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
DOWNLOADER="$SCRIPT_DIR/download-firecracker-kernel.sh"
PIN="932450603af9175c443f5348aa961326945e3b4b46ba34bab2c714c751ee2f85"

if [ ! -f "$DOWNLOADER" ]; then
    echo "FAIL: downloader not found at $DOWNLOADER" >&2
    exit 1
fi

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

HOME_DIR="$WORKDIR/home"
FAKEBIN="$WORKDIR/bin"
TMP_DIR="$WORKDIR/tmp"
mkdir -p "$HOME_DIR" "$FAKEBIN" "$TMP_DIR"

printf 'good kernel virtio_rng\n' > "$WORKDIR/good-bytes"
printf 'bad kernel bytes\n' > "$WORKDIR/bad-bytes"
printf 'old kernel without rng driver\n' > "$WORKDIR/old-bytes"
printf 'custom virtio_rng kernel\n' > "$WORKDIR/custom-bytes"

GOOD_SHA=$(sha256sum -- "$WORKDIR/good-bytes" | awk '{print $1}')
BAD_SHA=$(sha256sum -- "$WORKDIR/bad-bytes" | awk '{print $1}')
CUSTOM_SHA=$(sha256sum -- "$WORKDIR/custom-bytes" | awk '{print $1}')

cat > "$FAKEBIN/curl" << 'EOF'
#!/bin/bash
set -euo pipefail
if [ -n "${FAKE_CURL_MARKER:-}" ]; then
    printf 'invoked\n' >> "$FAKE_CURL_MARKER"
fi
if [ "${FAKE_CURL_FAIL:-}" = "1" ]; then
    echo "fake curl: forced failure" >&2
    exit 1
fi
out=""
prev=""
for arg in "$@"; do
    if [ "$prev" = "-o" ]; then
        out=$arg
    fi
    prev=$arg
done
if [ -z "$out" ]; then
    echo "fake curl: missing -o" >&2
    exit 1
fi
if [ -z "${FAKE_CURL_SRC:-}" ] || [ ! -f "$FAKE_CURL_SRC" ]; then
    echo "fake curl: FAKE_CURL_SRC is not a file" >&2
    exit 1
fi
cp -- "$FAKE_CURL_SRC" "$out"
EOF
chmod 755 "$FAKEBIN/curl"

cat > "$FAKEBIN/wget" << 'EOF'
#!/bin/bash
echo "fake wget: network is disabled in this test" >&2
exit 1
EOF
chmod 755 "$FAKEBIN/wget"

FAKE_CURL_SRC=""
FAKE_CURL_MARKER="$WORKDIR/curl-invocations"
FAKE_CURL_FAIL=""
: > "$FAKE_CURL_MARKER"

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_contains() {
    local haystack="$1"
    local needle="$2"
    local msg="$3"
    if ! printf '%s\n' "$haystack" | grep -F -q -- "$needle"; then
        printf 'FAIL: %s\nmissing: %s\n--- output ---\n%s\n' "$msg" "$needle" "$haystack" >&2
        exit 1
    fi
}

assert_not_contains() {
    local haystack="$1"
    local needle="$2"
    local msg="$3"
    if printf '%s\n' "$haystack" | grep -F -q -- "$needle"; then
        printf 'FAIL: %s\nunexpected: %s\n--- output ---\n%s\n' "$msg" "$needle" "$haystack" >&2
        exit 1
    fi
}

reset_marker() {
    : > "$FAKE_CURL_MARKER"
    FAKE_CURL_FAIL=""
}

# dest is the install path. Pass an empty string to exercise the HOME fallback.
# Extra arguments are KEY=VALUE pairs for env.
run_at() {
    local dest="$1"
    shift
    local -a env_args=()
    if [ -n "$dest" ]; then
        env_args+=(AEGIS_KERNEL_PATH="$dest")
    fi
    env -i \
        HOME="$HOME_DIR" \
        PATH="$FAKEBIN:/usr/bin:/bin" \
        TMPDIR="$TMP_DIR" \
        FAKE_CURL_SRC="$FAKE_CURL_SRC" \
        FAKE_CURL_MARKER="$FAKE_CURL_MARKER" \
        FAKE_CURL_FAIL="$FAKE_CURL_FAIL" \
        "$@" \
        "${env_args[@]}" \
        bash "$DOWNLOADER"
}

CAPTURE=""
CAPTURE_RC=0
run_capture() {
    local dest="$1"
    shift
    if CAPTURE=$(run_at "$dest" "$@" 2>&1); then
        CAPTURE_RC=0
    else
        CAPTURE_RC=$?
    fi
}

assert_no_temp_leftovers() {
    local dir="$1"
    local leftovers
    leftovers=$(find "$dir" -name '.vmlinux.download.*' -print)
    if [ -n "$leftovers" ]; then
        fail "temp download left behind: $leftovers"
    fi
}

# The pinned constant is what a default-URL download is checked against.
if ! grep -F -q -- "KERNEL_SHA256=\"$PIN\"" "$DOWNLOADER"; then
    fail "downloader is missing the pinned KERNEL_SHA256"
fi
if ! grep -F -q -- "$PIN" "$REPO_ROOT/README.md"; then
    fail "README is missing the pinned kernel SHA-256"
fi
if ! grep -F -q -- "firecracker-v1.15.1-x86_64.tgz.sha256.txt" "$REPO_ROOT/README.md"; then
    fail "README does not verify the Firecracker tarball via the published .sha256.txt"
fi

# (a) mismatch is rejected and not installed
reset_marker
dest="$WORKDIR/mismatch/vmlinux"
mkdir -p "$(dirname "$dest")"
FAKE_CURL_SRC="$WORKDIR/bad-bytes"
run_capture "$dest"
if [ "$CAPTURE_RC" -eq 0 ]; then
    fail "mismatched download exited 0"
fi
if [ -e "$dest" ]; then
    fail "mismatched download installed a kernel"
fi
assert_no_temp_leftovers "$WORKDIR/mismatch"
assert_contains "$CAPTURE" "Expected: $PIN" "mismatch did not report the pinned hash"
assert_contains "$CAPTURE" "Actual:   $BAD_SHA" "mismatch did not report the actual hash"
assert_contains "$CAPTURE" "not installed" "mismatch did not say the kernel was not installed"
if [ ! -s "$FAKE_CURL_MARKER" ]; then
    fail "mismatch case did not attempt a download"
fi
echo "ok: mismatched download rejected"

# A failed re-download must not delete an older kernel that lacked virtio_rng.
reset_marker
dest="$WORKDIR/replace/vmlinux"
mkdir -p "$(dirname "$dest")"
cp -- "$WORKDIR/old-bytes" "$dest"
FAKE_CURL_SRC="$WORKDIR/bad-bytes"
run_capture "$dest"
if [ "$CAPTURE_RC" -eq 0 ]; then
    fail "re-download of a mismatched kernel exited 0"
fi
if ! cmp -s -- "$WORKDIR/old-bytes" "$dest"; then
    fail "failed re-download replaced or deleted the previous kernel"
fi
assert_no_temp_leftovers "$WORKDIR/replace"
echo "ok: failed re-download left the previous kernel in place"

# (b) matching download is installed (explicit path and HOME fallback)
reset_marker
dest="$WORKDIR/match/vmlinux"
mkdir -p "$(dirname "$dest")"
FAKE_CURL_SRC="$WORKDIR/good-bytes"
run_capture "$dest" "AEGIS_KERNEL_SHA256=$GOOD_SHA"
if [ "$CAPTURE_RC" -ne 0 ]; then
    fail "matching download failed rc=$CAPTURE_RC output=$CAPTURE"
fi
if ! cmp -s -- "$WORKDIR/good-bytes" "$dest"; then
    fail "matching download did not install the fetched bytes"
fi
mode=$(stat -c '%a' "$dest")
if [ "$mode" != "644" ]; then
    fail "installed kernel mode is $mode, want 644"
fi
assert_contains "$CAPTURE" "SHA-256 verified." "match did not confirm verification"
assert_no_temp_leftovers "$WORKDIR/match"
echo "ok: matching download installed"

# Second run sees virtio_rng and the same hash, so it must not download again.
reset_marker
FAKE_CURL_SRC="$WORKDIR/bad-bytes"
run_capture "$dest" "AEGIS_KERNEL_SHA256=$GOOD_SHA"
if [ "$CAPTURE_RC" -ne 0 ]; then
    fail "idempotent re-run failed rc=$CAPTURE_RC output=$CAPTURE"
fi
if ! cmp -s -- "$WORKDIR/good-bytes" "$dest"; then
    fail "idempotent re-run changed the installed kernel"
fi
if [ -s "$FAKE_CURL_MARKER" ]; then
    fail "idempotent re-run downloaded again"
fi
assert_contains "$CAPTURE" "matches the pinned hash" "idempotent re-run did not confirm the hash"
echo "ok: existing matching kernel skipped the download"

# HOME fallback uses $HOME/.aegis/firecracker/vmlinux, never the real home.
reset_marker
FAKE_CURL_SRC="$WORKDIR/good-bytes"
run_capture "" "AEGIS_KERNEL_SHA256=$GOOD_SHA"
fallback="$HOME_DIR/.aegis/firecracker/vmlinux"
if [ "$CAPTURE_RC" -ne 0 ]; then
    fail "HOME fallback download failed rc=$CAPTURE_RC output=$CAPTURE"
fi
if ! cmp -s -- "$WORKDIR/good-bytes" "$fallback"; then
    fail "HOME fallback did not install under the temp HOME"
fi
echo "ok: HOME fallback installed under the temp HOME"

# (c) skip flag installs a non-matching file and says so loudly
reset_marker
dest="$WORKDIR/skip/vmlinux"
mkdir -p "$(dirname "$dest")"
FAKE_CURL_SRC="$WORKDIR/bad-bytes"
run_capture "$dest" "AEGIS_SKIP_KERNEL_CHECKSUM=1"
if [ "$CAPTURE_RC" -ne 0 ]; then
    fail "skip flag failed rc=$CAPTURE_RC output=$CAPTURE"
fi
if ! cmp -s -- "$WORKDIR/bad-bytes" "$dest"; then
    fail "skip flag did not install the downloaded bytes"
fi
assert_contains "$CAPTURE" "AEGIS_SKIP_KERNEL_CHECKSUM=1" "skip flag was not announced"
assert_contains "$CAPTURE" "SKIPPING SHA-256" "skip flag was not loud"
assert_contains "$CAPTURE" "UNVERIFIED" "skip flag did not say the bytes are unverified"
assert_not_contains "$CAPTURE" "not installed" "skip flag rejected the download"
assert_no_temp_leftovers "$WORKDIR/skip"
echo "ok: skip flag installed without verification"

# Existing virtio_rng kernel with a different hash: warn, keep, do not download.
reset_marker
dest="$WORKDIR/custom/vmlinux"
mkdir -p "$(dirname "$dest")"
cp -- "$WORKDIR/custom-bytes" "$dest"
FAKE_CURL_SRC="$WORKDIR/bad-bytes"
run_capture "$dest"
if [ "$CAPTURE_RC" -ne 0 ]; then
    fail "custom existing kernel failed rc=$CAPTURE_RC output=$CAPTURE"
fi
if ! cmp -s -- "$WORKDIR/custom-bytes" "$dest"; then
    fail "custom existing kernel was deleted or replaced"
fi
if [ -s "$FAKE_CURL_MARKER" ]; then
    fail "custom existing kernel triggered a download"
fi
assert_contains "$CAPTURE" "deliberately custom kernel" "custom kernel warning missing"
assert_contains "$CAPTURE" "not deleting it" "custom kernel warning did not promise to keep the file"
assert_contains "$CAPTURE" "Expected: $PIN" "custom kernel warning missing the pin"
assert_contains "$CAPTURE" "Actual:   $CUSTOM_SHA" "custom kernel warning missing the actual hash"
echo "ok: custom existing kernel kept with a warning"

# Override URL without a hash or the skip flag must fail before any download.
reset_marker
dest="$WORKDIR/override/vmlinux"
mkdir -p "$(dirname "$dest")"
FAKE_CURL_SRC="$WORKDIR/good-bytes"
run_capture "$dest" "AEGIS_KERNEL_URL=file:///kernel-fixture-not-fetched"
if [ "$CAPTURE_RC" -eq 0 ]; then
    fail "URL override without a checksum exited 0"
fi
if [ -e "$dest" ]; then
    fail "URL override without a checksum installed a kernel"
fi
if [ -s "$FAKE_CURL_MARKER" ]; then
    fail "URL override without a checksum downloaded anyway"
fi
assert_contains "$CAPTURE" "AEGIS_KERNEL_SHA256" "URL override error did not mention the hash"
assert_contains "$CAPTURE" "AEGIS_SKIP_KERNEL_CHECKSUM=1" "URL override error did not mention the skip flag"
echo "ok: URL override without a checksum was rejected"

# Override URL plus its own hash installs, and is not checked against the upstream pin.
reset_marker
dest="$WORKDIR/mirror/vmlinux"
mkdir -p "$(dirname "$dest")"
FAKE_CURL_SRC="$WORKDIR/good-bytes"
run_capture "$dest" \
    "AEGIS_KERNEL_URL=file:///kernel-fixture-not-fetched" \
    "AEGIS_KERNEL_SHA256=$GOOD_SHA"
if [ "$CAPTURE_RC" -ne 0 ]; then
    fail "URL override with checksum failed rc=$CAPTURE_RC output=$CAPTURE"
fi
if ! cmp -s -- "$WORKDIR/good-bytes" "$dest"; then
    fail "URL override with checksum did not install the fetched bytes"
fi
echo "ok: URL override with checksum installed"

echo "all kernel checksum checks passed"
