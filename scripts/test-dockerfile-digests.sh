#!/bin/bash
# Fail unless every tracked Dockerfile pins external FROM images by digest.
# Tag-only bases drift. To bump a pin, read the multi-arch index digest with
# `docker buildx imagetools inspect <image>:<tag>` (or the registry API) and
# update the tag and the digest together. See README "Docker base image digests".
#
# scratch and a FROM that names an earlier stage alias are not external images.
# go install @latest is rejected. A wget or curl download in a RUN must run
# sha256sum -c in that same RUN.
#
# The script scans its own fixtures before the repo, so a case-sensitive FROM
# match fails the lowercase fixture instead of passing silently.

set -euo pipefail

CHECK_FROMS=0
SELF_WORK=""
# Single-quoted so the backtick stays a regex atom, not command substitution.
GO_INSTALL_RE='(^|[[:space:];|&`])go[[:space:]]+install[[:space:]]'
DOWNLOAD_RE='(^|[[:space:];|&`])(wget|curl)([[:space:]]|$)'
CHECKSUM_RE='sha256sum[[:space:]]+(-c|--check)([[:space:]]|$)'

cleanup_self_work() {
    if [ -n "${SELF_WORK}" ] && [ -d "${SELF_WORK}" ]; then
        rm -rf -- "${SELF_WORK}"
    fi
}
trap cleanup_self_work EXIT

# Join backslash continuations so a checksum later in the same RUN is visible.
emit_logical_lines() {
    local file="$1"
    local line="" acc=""
    while IFS= read -r line || [ -n "$line" ]; do
        line=${line%$'\r'}
        if [[ "$line" == *\\ ]]; then
            acc+="${line%\\}"
            continue
        fi
        acc+="$line"
        printf '%s\n' "$acc"
        acc=""
    done < "$file"
    if [ -n "$acc" ]; then
        printf '%s\n' "$acc"
    fi
}

is_git_worktree_root() {
    local root="$1"
    local top="" here=""
    top=$(git -C "$root" rev-parse --show-toplevel 2>/dev/null) || return 1
    here=$(cd "$root" && pwd)
    top=$(cd "$top" && pwd)
    [ "$top" = "$here" ]
}

# Names: Dockerfile, Dockerfile.*, *.Dockerfile. Paths are newline-separated.
list_dockerfiles() {
    local root="$1"
    local rel="" base=""
    if is_git_worktree_root "$root"; then
        while IFS= read -r rel || [ -n "${rel:-}" ]; do
            [ -z "${rel:-}" ] && continue
            base=${rel##*/}
            case "$base" in
                Dockerfile|Dockerfile.*|*.Dockerfile)
                    printf '%s\n' "$root/$rel"
                    ;;
            esac
        done < <(git -C "$root" ls-files)
    else
        find "$root" -type f \( \
            -name 'Dockerfile' -o \
            -name '*.Dockerfile' -o \
            -name 'Dockerfile.*' \
        \) -print
    fi
}

check_one() {
    local file="$1"
    local root="$2"
    local line="" trimmed="" rest="" image="" tok="" stage_name="" lower="" rel="" s=""
    local i=0 next=0 name_i=0 froms=0 status=0 is_from=0 allowed=0
    local -a tokens=()
    local -a stages=()

    rel="${file#"$root"/}"
    if [ ! -r "$file" ]; then
        echo "FAIL: cannot read ${rel}" >&2
        CHECK_FROMS=0
        return 1
    fi

    while IFS= read -r line || [ -n "$line" ]; do
        trimmed="${line#"${line%%[![:space:]]*}"}"
        case "$trimmed" in
            ''|'#'*)
                continue
                ;;
        esac

        is_from=0
        rest=""
        if [[ "$trimmed" =~ ^[Ff][Rr][Oo][Mm]$ ]]; then
            is_from=1
        elif [[ "$trimmed" =~ ^[Ff][Rr][Oo][Mm][[:space:]]+(.*)$ ]]; then
            is_from=1
            rest="${BASH_REMATCH[1]}"
        fi

        if [ "$is_from" -eq 1 ]; then
            tokens=()
            if [ -n "$rest" ]; then
                read -r -a tokens <<< "$rest"
            fi
            i=0
            image=""
            while [ "$i" -lt "${#tokens[@]}" ]; do
                tok="${tokens[$i]}"
                if [[ "$tok" == --* ]]; then
                    if [[ "$tok" == *=* ]]; then
                        i=$((i + 1))
                    else
                        i=$((i + 2))
                    fi
                    continue
                fi
                image=$tok
                break
            done
            froms=$((froms + 1))
            stage_name=""
            if [ -n "$image" ]; then
                next=$((i + 1))
                if [ "$next" -lt "${#tokens[@]}" ] && [[ "${tokens[$next]}" =~ ^[Aa][Ss]$ ]]; then
                    name_i=$((next + 1))
                    if [ "$name_i" -lt "${#tokens[@]}" ]; then
                        stage_name="${tokens[$name_i]}"
                    fi
                fi
            fi

            allowed=0
            if [ -n "$image" ]; then
                lower=$(printf '%s' "$image" | tr '[:upper:]' '[:lower:]')
                if [ "$lower" = "scratch" ]; then
                    allowed=1
                elif [ "${#stages[@]}" -gt 0 ]; then
                    for s in "${stages[@]}"; do
                        if [ "$s" = "$image" ]; then
                            allowed=1
                            break
                        fi
                    done
                fi
            fi
            if [ "$allowed" -eq 0 ]; then
                if [ -z "$image" ] || [[ ! "$image" =~ @sha256:[0-9a-fA-F]{64}([^0-9a-fA-F]|$) ]]; then
                    echo "FAIL: ${rel} has unpinned FROM: ${trimmed}" >&2
                    status=1
                fi
            fi
            if [ -n "$stage_name" ]; then
                stages+=("$stage_name")
            fi
            continue
        fi

        if [[ "$trimmed" =~ $GO_INSTALL_RE ]] && [[ "$trimmed" == *@latest* ]]; then
            echo "FAIL: ${rel} has go install @latest: ${trimmed}" >&2
            status=1
        fi

        if [[ "$trimmed" =~ ^[Rr][Uu][Nn][[:space:]] ]] \
            && [[ "$trimmed" =~ $DOWNLOAD_RE ]] \
            && [[ ! "$trimmed" =~ $CHECKSUM_RE ]]; then
            echo "FAIL: ${rel} downloads with wget or curl but the RUN does not run sha256sum -c: ${trimmed}" >&2
            status=1
        fi
    done < <(emit_logical_lines "$file")

    CHECK_FROMS=$froms
    return "$status"
}

check_tree() {
    local root="$1"
    local file=""
    local froms=0 fail=0 nfiles=0
    local -a files=()

    while IFS= read -r file || [ -n "${file:-}" ]; do
        [ -z "${file:-}" ] && continue
        files+=("$file")
    done < <(list_dockerfiles "$root" | LC_ALL=C sort)

    if [ "${#files[@]}" -eq 0 ]; then
        echo "FAIL: no Dockerfiles found under ${root}" >&2
        return 1
    fi

    for file in "${files[@]}"; do
        nfiles=$((nfiles + 1))
        if ! check_one "$file" "$root"; then
            fail=1
        fi
        froms=$((froms + CHECK_FROMS))
    done

    if [ "$froms" -eq 0 ]; then
        echo "FAIL: no FROM instructions found under ${root}" >&2
        return 1
    fi
    if [ "$fail" -ne 0 ]; then
        return 1
    fi
    echo "ok: ${froms} FROM instructions across ${nfiles} Dockerfiles are digest-pinned"
}

expect_fail() {
    local dir="$1"
    local needle="$2"
    local label="$3"
    local out=""
    if out=$(check_tree "$dir" 2>&1); then
        printf 'FAIL: self-test %s: expected rejection, got success:\n%s\n' "$label" "$out" >&2
        return 1
    fi
    if ! printf '%s\n' "$out" | grep -F -q -- "$needle"; then
        printf 'FAIL: self-test %s: missing %s\n--- output ---\n%s\n' "$label" "$needle" "$out" >&2
        return 1
    fi
    printf 'ok: self-test %s\n' "$label"
}

expect_pass() {
    local dir="$1"
    local label="$2"
    local out=""
    if ! out=$(check_tree "$dir" 2>&1); then
        printf 'FAIL: self-test %s: expected pass:\n%s\n' "$label" "$out" >&2
        return 1
    fi
    if printf '%s\n' "$out" | grep -F -q -- 'FAIL:'; then
        printf 'FAIL: self-test %s: passed with a FAIL line:\n%s\n' "$label" "$out" >&2
        return 1
    fi
    printf 'ok: self-test %s\n' "$label"
}

run_self_tests() {
    local pin="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    SELF_WORK=$(mktemp -d)
    local work="$SELF_WORK"

    mkdir -p "$work/lower/cmd/demo"
    printf '  from alpine:3.18\n' > "$work/lower/cmd/demo/Dockerfile"
    expect_fail "$work/lower" "from alpine:3.18" "lowercase from rejected"

    mkdir -p "$work/mixed/cmd/demo"
    printf 'From alpine:3.18\n' > "$work/mixed/cmd/demo/Dockerfile"
    expect_fail "$work/mixed" "From alpine:3.18" "mixed-case From rejected"

    mkdir -p "$work/outside/deploy"
    printf 'FROM ubuntu:22.04\n' > "$work/outside/deploy/Dockerfile"
    expect_fail "$work/outside" "ubuntu:22.04" "Dockerfile outside cmd/ rejected"

    mkdir -p "$work/forward/cmd/demo"
    printf 'FROM builder\n' > "$work/forward/cmd/demo/Dockerfile"
    expect_fail "$work/forward" "FROM builder" "undefined stage alias rejected"

    mkdir -p "$work/stages/cmd/demo"
    cat > "$work/stages/cmd/demo/Dockerfile" << EOF
FROM alpine:3.18@sha256:${pin} AS builder
FROM --platform=linux/amd64 builder
FROM scratch
from scratch AS empty
FROM --platform=linux/amd64 scratch
EOF
    expect_pass "$work/stages" "stage alias and scratch accepted"

    mkdir -p "$work/pinned/cmd/demo" "$work/pinned/notes"
    cat > "$work/pinned/cmd/demo/Dockerfile" << EOF
  From --platform=linux/amd64 alpine:3.18@sha256:${pin}
FROM --platform linux/amd64 alpine:3.18@sha256:${pin} AS base
RUN go install example.com/tool/cmd/tool@v1.2.3
EOF
    printf 'from alpine:3.18\n' > "$work/pinned/notes/readme.txt"
    expect_pass "$work/pinned" "pinned FROM accepted"

    mkdir -p "$work/latest/cmd/demo"
    cat > "$work/latest/cmd/demo/Dockerfile" << EOF
FROM alpine:3.18@sha256:${pin}
RUN go install example.com/tool/cmd/tool@latest
EOF
    expect_fail "$work/latest" "@latest" "go install @latest rejected"

    mkdir -p "$work/wget/cmd/demo"
    cat > "$work/wget/cmd/demo/Dockerfile" << EOF
FROM alpine:3.18@sha256:${pin}
RUN wget -q https://example.invalid/tool -O /tmp/tool && chmod +x /tmp/tool
EOF
    expect_fail "$work/wget" "sha256sum -c" "wget without checksum rejected"

    mkdir -p "$work/wget-ok/packaging"
    cat > "$work/wget-ok/packaging/tool.Dockerfile" << EOF
FROM alpine:3.18@sha256:${pin}
RUN wget -q https://example.invalid/tool -O /tmp/tool \\
 && echo "${pin}  /tmp/tool" | sha256sum -c - \\
 && chmod +x /tmp/tool
EOF
    expect_pass "$work/wget-ok" "wget with sha256sum -c accepted"

    cleanup_self_work
    SELF_WORK=""
}

run_self_tests

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
check_tree "$REPO_ROOT"
