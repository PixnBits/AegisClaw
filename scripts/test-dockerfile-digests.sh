#!/bin/bash
# Fail unless every tracked Dockerfile pins external FROM images by digest.
# Tag-only bases drift. To bump a pin, read the multi-arch index digest with
# `docker buildx imagetools inspect <image>:<tag>` (or the registry API) and
# update the tag and the digest together. See README "Docker base image digests".
#
# scratch and a FROM that names an earlier stage alias are not external images.
# Also rejected:
#   - a floating ref in a RUN (@latest, @master, @main, @HEAD), e.g. go install
#   - an ARG whose default is latest, master, main or HEAD
#   - a wget or curl download in a RUN that doesn't run sha256sum -c in that
#     same RUN (shell form or exec form, RUN ["wget", ...])
#   - ADD of an http(s) URL without --checksum=sha256:...
#   - COPY --from=<image> that is neither an earlier stage, a stage index, nor
#     digest-pinned
#   - npm/yarn/pnpm package installs without an exact version, and pip
#     installs without ==version (pip -r needs --require-hashes)
#
# The script scans its own fixtures before the repo, so a case-sensitive FROM
# match fails the lowercase fixture instead of passing silently.

set -euo pipefail

CHECK_FROMS=0
SELF_WORK=""
# Single-quoted so the backtick stays a regex atom, not command substitution.
DOWNLOAD_RE='(^|[[:space:];|&`])(wget|curl)([[:space:]]|$)'
CHECKSUM_RE='sha256sum[[:space:]]+(-c|--check)([[:space:]]|$)'
FLOATING_REF_RE='@(latest|master|main|HEAD)([^A-Za-z0-9._/-]|$)'
DIGEST_RE='@sha256:[0-9a-fA-F]{64}([^0-9a-fA-F]|$)'

# RUN/ADD body with exec-form JSON punctuation turned into spaces, so
# RUN ["wget", "url"] is matched like RUN wget url. Leading --flags
# (--mount=..., --network=...) are dropped.
normalize_instruction_body() {
    local body="$1"
    local -a toks=()
    local out="" t="" seen_cmd=0
    body=${body//[\[\]]/ }
    body=${body//\"/ }
    body=${body//,/ }
    read -r -a toks <<< "$body"
    for t in "${toks[@]}"; do
        if [ "$seen_cmd" -eq 0 ] && [[ "$t" == --* ]]; then
            continue
        fi
        seen_cmd=1
        out+="$t "
    done
    printf '%s' "${out% }"
}

# Prints one reason per unpinned npm/yarn/pnpm or pip install in a normalized
# RUN body. Commands are split on && || ; |.
unpinned_installs() {
    local body="$1"
    local seg="" t="" mgr="" pkg="" name="" ver="" skip_next=0 req_file=0 req_hashes=0
    local i=0
    local -a toks=()
    body=${body//&&/;}
    body=${body//||/;}
    body=${body//|/;}
    while IFS= read -r seg; do
        read -r -a toks <<< "$seg"
        mgr=""
        i=0
        while [ "$i" -lt "${#toks[@]}" ]; do
            t=${toks[$i]}
            if { [ "$t" = "npm" ] || [ "$t" = "pnpm" ] || [ "$t" = "yarn" ]; } \
                && [ $((i + 1)) -lt "${#toks[@]}" ]; then
                case "${toks[$((i + 1))]}" in
                    install|i|add|in|ins)
                        mgr=node
                        i=$((i + 2))
                        break
                        ;;
                esac
            fi
            if { [ "$t" = "pip" ] || [ "$t" = "pip3" ]; } \
                && [ $((i + 1)) -lt "${#toks[@]}" ] && [ "${toks[$((i + 1))]}" = "install" ]; then
                mgr=pip
                i=$((i + 2))
                break
            fi
            i=$((i + 1))
        done
        [ -z "$mgr" ] && continue
        skip_next=0
        req_file=0
        req_hashes=0
        while [ "$i" -lt "${#toks[@]}" ]; do
            t=${toks[$i]}
            i=$((i + 1))
            if [ "$skip_next" -eq 1 ]; then
                skip_next=0
                continue
            fi
            if [ "$mgr" = pip ]; then
                case "$t" in
                    -r|--requirement|-c|--constraint)
                        req_file=1
                        skip_next=1
                        continue
                        ;;
                    --require-hashes)
                        req_hashes=1
                        continue
                        ;;
                    -i|--index-url|--extra-index-url|-t|--target|--prefix|--root|-f|--find-links)
                        skip_next=1
                        continue
                        ;;
                esac
            else
                case "$t" in
                    --prefix|--registry|--cache|-C|--dir)
                        skip_next=1
                        continue
                        ;;
                esac
            fi
            case "$t" in
                -*|.|./*|/*|*.tgz|*.tar.gz|*.whl)
                    continue
                    ;;
            esac
            pkg=$t
            if [ "$mgr" = pip ]; then
                ver=${pkg#*==}
                if [[ "$pkg" != *==* ]] || [ -z "$ver" ] || [[ "$ver" == *\** ]]; then
                    printf 'pip install without ==version: %s\n' "$pkg"
                fi
            else
                if [[ "$pkg" == @*/* ]]; then
                    name=${pkg#@}
                    name="@${name%%@*}"
                else
                    name=${pkg%%@*}
                fi
                ver=${pkg#"$name"}
                ver=${ver#@}
                if [ "$ver" = "$pkg" ] || [ -z "$ver" ] || [[ ! "$ver" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]]; then
                    printf 'node package install without an exact version: %s\n' "$pkg"
                fi
            fi
        done
        if [ "$mgr" = pip ] && [ "$req_file" -eq 1 ] && [ "$req_hashes" -eq 0 ]; then
            printf 'pip install -r/-c without --require-hashes\n'
        fi
    done <<< "${body//;/$'\n'}"
}

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
    local argval="" body="" raw_body="" from_ref="" reason=""
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

        if [[ "$trimmed" =~ ^[Aa][Rr][Gg][[:space:]]+([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]]; then
            argval=${BASH_REMATCH[2]}
            argval=${argval//\"/}
            argval=${argval//\'/}
            argval=${argval%%[[:space:]]*}
            case "$(printf '%s' "$argval" | tr '[:upper:]' '[:lower:]')" in
                latest|master|main|head)
                    echo "FAIL: ${rel} has an ARG defaulting to a floating ref (${argval}): ${trimmed}" >&2
                    status=1
                    ;;
            esac
            continue
        fi

        if [[ "$trimmed" =~ ^[Rr][Uu][Nn][[:space:]]+(.*)$ ]]; then
            body=$(normalize_instruction_body "${BASH_REMATCH[1]}")
            if [[ "$body" =~ $FLOATING_REF_RE ]]; then
                echo "FAIL: ${rel} has a floating ref (@${BASH_REMATCH[1]}) in a RUN: ${trimmed}" >&2
                status=1
            fi
            if [[ "$body" =~ $DOWNLOAD_RE ]] && [[ ! "$body" =~ $CHECKSUM_RE ]]; then
                echo "FAIL: ${rel} downloads with wget or curl but the RUN does not run sha256sum -c: ${trimmed}" >&2
                status=1
            fi
            while IFS= read -r reason; do
                [ -z "$reason" ] && continue
                echo "FAIL: ${rel} has an unpinned package install (${reason}): ${trimmed}" >&2
                status=1
            done < <(unpinned_installs "$body")
            continue
        fi

        if [[ "$trimmed" =~ ^[Aa][Dd][Dd][[:space:]]+(.*)$ ]]; then
            raw_body=${BASH_REMATCH[1]}
            body=$(normalize_instruction_body "$raw_body")
            if [[ " $body " =~ [[:space:]][Hh][Tt][Tt][Pp][Ss]?:// ]] \
                && [[ ! "$raw_body" =~ --checksum=sha256:[0-9a-fA-F]{64} ]]; then
                echo "FAIL: ${rel} ADDs a URL without --checksum=sha256:...: ${trimmed}" >&2
                status=1
            fi
            continue
        fi

        if [[ "$trimmed" =~ ^[Cc][Oo][Pp][Yy][[:space:]]+(.*)$ ]]; then
            tokens=()
            read -r -a tokens <<< "${BASH_REMATCH[1]}"
            from_ref=""
            i=0
            while [ "$i" -lt "${#tokens[@]}" ]; do
                tok=${tokens[$i]}
                if [[ "$tok" == --from=* ]]; then
                    from_ref=${tok#--from=}
                elif [ "$tok" = "--from" ] && [ $((i + 1)) -lt "${#tokens[@]}" ]; then
                    from_ref=${tokens[$((i + 1))]}
                fi
                i=$((i + 1))
            done
            if [ -n "$from_ref" ]; then
                allowed=0
                if [[ "$from_ref" =~ ^[0-9]+$ ]] || [[ "$from_ref" =~ $DIGEST_RE ]]; then
                    allowed=1
                elif [ "${#stages[@]}" -gt 0 ]; then
                    for s in "${stages[@]}"; do
                        if [ "$s" = "$from_ref" ]; then
                            allowed=1
                            break
                        fi
                    done
                fi
                if [ "$allowed" -eq 0 ]; then
                    echo "FAIL: ${rel} has COPY --from an unpinned image (${from_ref}): ${trimmed}" >&2
                    status=1
                fi
            fi
            continue
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

    # One Dockerfile per case so each rejection is attributed to its own rule.
    reject_case() {
        local name="$1" needle="$2" label="$3" body="$4"
        mkdir -p "$work/$name/cmd/demo"
        printf 'FROM alpine:3.18@sha256:%s AS base\n%s\n' "$pin" "$body" > "$work/$name/cmd/demo/Dockerfile"
        expect_fail "$work/$name" "$needle" "$label"
    }
    accept_case() {
        local name="$1" label="$2" body="$3"
        mkdir -p "$work/$name/cmd/demo"
        printf 'FROM alpine:3.18@sha256:%s AS base\n%s\n' "$pin" "$body" > "$work/$name/cmd/demo/Dockerfile"
        expect_pass "$work/$name" "$label"
    }

    # shellcheck disable=SC2016 # ${TOOL_VERSION} is Dockerfile text, not shell.
    reject_case arg-latest "floating ref (latest)" "ARG latest default rejected" \
        'ARG TOOL_VERSION=latest
RUN go install example.com/tool/cmd/tool@${TOOL_VERSION}'
    reject_case arg-main-quoted "floating ref (main)" "ARG quoted main default rejected" \
        'ARG REF="main"'
    reject_case ref-master "@master" "go install @master rejected" \
        'RUN go install example.com/tool/cmd/tool@master'
    reject_case ref-main "@main" "go install @main rejected" \
        'RUN go install example.com/tool/cmd/tool@main'
    reject_case ref-head "@HEAD" "go install @HEAD rejected" \
        'RUN go install example.com/tool/cmd/tool@HEAD'
    reject_case add-url "ADDs a URL" "ADD https without --checksum rejected" \
        'ADD https://example.invalid/tool.tar.gz /tmp/'
    reject_case add-url-weak "ADDs a URL" "ADD --checksum without sha256 rejected" \
        'ADD --checksum=md5:abc https://example.invalid/tool.tar.gz /tmp/'
    reject_case exec-wget "sha256sum -c" "exec-form RUN wget rejected" \
        'RUN ["wget", "-q", "https://example.invalid/tool", "-O", "/tmp/tool"]'
    reject_case exec-curl-sh "sha256sum -c" "exec-form RUN sh -c curl rejected" \
        'RUN ["/bin/sh", "-c", "curl -fsSL https://example.invalid/tool -o /tmp/tool"]'
    reject_case run-mount-curl "sha256sum -c" "RUN --mount curl rejected" \
        'RUN --mount=type=cache,target=/root/.cache curl -fsSL https://example.invalid/t -o /t'
    reject_case exec-go-latest "@latest" "exec-form go install @latest rejected" \
        'RUN ["go", "install", "example.com/tool/cmd/tool@latest"]'
    reject_case npm-unpinned "node package install without an exact version: typescript" "npm install unpinned rejected" \
        'RUN npm install -g typescript'
    reject_case npm-tag "node package install without an exact version: typescript@next" "npm install @next tag rejected" \
        'RUN npm i -g typescript@next'
    reject_case npm-range "node package install without an exact version: left-pad@^1.3.0" "npm install range rejected" \
        'RUN npm install left-pad@^1.3.0'
    reject_case npm-scoped "node package install without an exact version: @scope/tool" "scoped npm install unpinned rejected" \
        'RUN cd app && npm add @scope/tool'
    reject_case yarn-unpinned "node package install without an exact version: esbuild" "yarn add unpinned rejected" \
        'RUN yarn add esbuild'
    reject_case pip-unpinned "pip install without ==version: requests" "pip install unpinned rejected" \
        'RUN pip install --no-cache-dir requests'
    reject_case pip-range "pip install without ==version: requests>=2" "pip install range rejected" \
        'RUN pip3 install "requests>=2"'
    reject_case pip-req "pip install -r/-c without --require-hashes" "pip -r without hashes rejected" \
        'RUN python3 -m pip install -r requirements.txt'
    reject_case exec-pip "pip install without ==version: requests" "exec-form pip install rejected" \
        'RUN ["pip", "install", "requests"]'
    reject_case copy-from-image "COPY --from an unpinned image (alpine:3.18)" "COPY --from tag-only image rejected" \
        'COPY --from=alpine:3.18 /etc/ssl /etc/ssl'
    reject_case copy-from-space "COPY --from an unpinned image (golang:1.26)" "COPY --from <image> (space form) rejected" \
        'COPY --from golang:1.26 /usr/local/go /usr/local/go'
    accept_case pinned-installs "pinned installs, stage COPY and ADD checksum accepted" \
        "ARG TOOL_VERSION=v1.2.3
ARG BRANCH_NOTE=maintenance
RUN npm ci && npm install -g typescript@5.6.3 @scope/tool@1.0.0-rc.1
RUN npm install
RUN cd web && npm run build
RUN pip install --no-cache-dir requests==2.32.3 -i https://pypi.org/simple
RUN pip install --require-hashes -r requirements.txt
RUN pip install .
RUN [\"go\", \"install\", \"example.com/tool/cmd/tool@v1.2.3\"]
ADD --checksum=sha256:${pin} https://example.invalid/tool.tar.gz /tmp/
ADD ./local.tar.gz /tmp/
COPY --from=base /etc/os-release /tmp/
COPY --from=0 /etc/os-release /tmp/
COPY --from=alpine:3.18@sha256:${pin} /etc/ssl /etc/ssl
RUN echo user@mainframe.example"

    cleanup_self_work
    SELF_WORK=""
}

run_self_tests

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
check_tree "$REPO_ROOT"
