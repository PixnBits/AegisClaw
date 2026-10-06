#!/bin/bash
# Fail if any cmd/*/Dockerfile FROM instruction lacks an @sha256: digest.
# Tag-only bases drift. To bump a pin, read the multi-arch index digest with
# `docker buildx imagetools inspect <image>:<tag>` (or the registry API) and
# update the tag and the digest together. See README "Docker base image digests".

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

shopt -s nullglob
files=("$REPO_ROOT"/cmd/*/Dockerfile)
if [ "${#files[@]}" -eq 0 ]; then
    echo "FAIL: no cmd/*/Dockerfile files found" >&2
    exit 1
fi

fail=0
found=0
for f in "${files[@]}"; do
    while IFS= read -r line || [ -n "$line" ]; do
        trimmed="${line#"${line%%[![:space:]]*}"}"
        case "$trimmed" in
            '' | '#'*)
                continue
                ;;
        esac
        case "$trimmed" in
            FROM[[:space:]]*)
                ;;
            *)
                continue
                ;;
        esac
        found=$((found + 1))
        if [[ ! "$trimmed" =~ @sha256:[0-9a-fA-F]{64}([^0-9a-fA-F]|$) ]]; then
            echo "FAIL: ${f#"$REPO_ROOT"/} has unpinned FROM: $trimmed" >&2
            fail=1
        fi
    done < "$f"
done

if [ "$found" -eq 0 ]; then
    echo "FAIL: no FROM instructions found under cmd/*/Dockerfile" >&2
    exit 1
fi
if [ "$fail" -ne 0 ]; then
    exit 1
fi
echo "ok: $found FROM instructions across ${#files[@]} Dockerfiles are digest-pinned"
