#!/bin/bash
# Fail unless every tracked Dockerfile pins external FROM images by digest.
# Tag-only bases drift. To bump a pin, read the multi-arch index digest with
# `docker buildx imagetools inspect <image>:<tag>` (or the registry API) and
# update the tag and the digest together. See README "Docker base image digests".
#
# scratch and a FROM that names an earlier stage alias are not external images.
# Also rejected:
#   - a floating ref in a RUN (@latest, @master, @main, @HEAD), e.g. go install
#   - an ARG whose default is latest, master, main or HEAD, when a later RUN
#     or ADD uses it as a ref (@$V, :$V, #$V, --branch $V, -b $V, or any
#     argument of a git command such as checkout, switch, fetch or reset).
#     Quotes are ignored, so @"$V" counts. An ARG such as BUILD_MODE=main
#     that is never used as a ref is fine, and a later ARG V=v1.2.3 clears
#     an earlier floating default.
#   - RUN heredoc bodies (RUN <<SH ... SH) get the same RUN checks. A heredoc
#     opens only where BuildKit opens one (see heredoc_markers), and one with
#     no terminator line is itself a failure.
#   - a wget or curl download in a RUN that doesn't run sha256sum -c in that
#     same RUN (shell form or exec form, RUN ["wget", ...])
#   - ADD of an http(s) URL without --checksum=sha256:...
#   - COPY --from=<image> that is neither an earlier stage, a stage index, nor
#     digest-pinned
#   - npm/yarn/pnpm package installs without an exact version, and pip
#     installs without ==version (pip -r/-c needs --require-hashes)
#   - a bare npm/yarn/pnpm install (no package names) unless a lockfile
#     (package-lock.json, npm-shrinkwrap.json, yarn.lock, pnpm-lock.yaml)
#     was copied earlier in the same stage (a COPY/ADD source, including a
#     glob or the whole build context) or bind-mounted into that RUN;
#     npm ci is preferred
#   - a bare pip install (nothing, or only a local project/wheel) without
#     -r/-c or --no-deps, since its dependencies resolve unpinned
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

# A RUN/ADD body normalized as above but with quotes deleted rather than
# turned into spaces, so @"$V" reads as @$V.
unquoted_body() {
    local body="$1"
    body=${body//\"/}
    body=${body//\'/}
    normalize_instruction_body "$body"
}

# Prints one reason per unpinned npm/yarn/pnpm or pip install in a normalized
# RUN body. Commands are split on && || ; |. $2 is 1 when this RUN has a node
# lockfile: copied earlier in the same stage, or bind-mounted into this RUN.
unpinned_installs() {
    local body="$1"
    local node_lock="${2:-0}"
    local seg="" t="" mgr="" tool="" pkg="" name="" ver="" skip_next=0 req_file=0 req_hashes=0
    local pkgs=0 no_deps=0 flag=""
    local i=0 j=0
    local -a toks=()
    # Quotes and backslashes hide the verb (npm 'install', n\pm).
    body=${body//\'/}
    body=${body//\\/}
    body=${body//&&/;}
    body=${body//||/;}
    body=${body//|/;}
    while IFS= read -r seg; do
        read -r -a toks <<< "$seg"
        mgr=""
        tool=""
        i=0
        while [ "$i" -lt "${#toks[@]}" ]; do
            t=${toks[$i]}
            tool=${t##*/}
            if [ "$tool" = "npm" ] || [ "$tool" = "pnpm" ] || [ "$tool" = "yarn" ]; then
                # Skip global flags between the tool and the verb. A flag that
                # takes a value consumes the next token; --flag=value is one.
                j=$((i + 1))
                while [ "$j" -lt "${#toks[@]}" ]; do
                    flag=${toks[$j]}
                    case "$flag" in
                        --prefix|--registry|--cache|-C|--dir|--cwd|--filter|--workspace|-w)
                            j=$((j + 2))
                            continue
                            ;;
                    esac
                    if [[ "$flag" == -* ]]; then
                        j=$((j + 1))
                        continue
                    fi
                    break
                done
                # yarn global add is an install. Bare yarn is yarn plus flags only.
                if [ "$tool" = "yarn" ] && [ "$j" -lt "${#toks[@]}" ] \
                    && [ "${toks[$j]}" = "global" ] \
                    && [ $((j + 1)) -lt "${#toks[@]}" ] \
                    && [ "${toks[$((j + 1))]}" = "add" ]; then
                    mgr=node
                    i=$((j + 2))
                    break
                fi
                if [ "$j" -lt "${#toks[@]}" ]; then
                    case "${toks[$j]}" in
                        install|i|in|ins|inst|insta|instal|isnt|isntal|isntall|add)
                            mgr=node
                            i=$((j + 1))
                            break
                            ;;
                    esac
                elif [ "$tool" = "yarn" ]; then
                    mgr=node
                    i=$j
                    break
                fi
            elif [[ "$tool" =~ ^pip[0-9.]*$ ]] \
                && [ $((i + 1)) -lt "${#toks[@]}" ] \
                && [ "${toks[$((i + 1))]}" = "install" ]; then
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
        pkgs=0
        no_deps=0
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
                    --no-deps)
                        no_deps=1
                        continue
                        ;;
                    -i|--index-url|--extra-index-url|-t|--target|--prefix|--root|-f|--find-links)
                        skip_next=1
                        continue
                        ;;
                esac
            else
                case "$t" in
                    --prefix|--registry|--cache|-C|--dir|--cwd|--filter|--workspace|-w)
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
            pkgs=$((pkgs + 1))
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
        if [ "$pkgs" -eq 0 ] && [ "$mgr" = node ] && [ "$node_lock" -eq 0 ]; then
            printf 'bare %s install without a lockfile (COPY package-lock.json first and use npm ci)\n' "$tool"
        fi
        if [ "$pkgs" -eq 0 ] && [ "$mgr" = pip ] && [ "$req_file" -eq 0 ] && [ "$no_deps" -eq 0 ]; then
            printf 'bare pip install without a lockfile (use -r/-c with --require-hashes, or --no-deps)\n'
        fi
    done <<< "${body//;/$'\n'}"
}

# True when a COPY or ADD source is a node lockfile, a glob that matches one
# (package-lock.json, npm-shrinkwrap.json, yarn.lock, pnpm-lock.yaml), or the
# whole build context (exactly "." or "./"). The destination is not counted.
has_node_lockfile() {
    local line="$1" t="" base="" name=""
    local -a toks=()
    local -a names=(package-lock.json npm-shrinkwrap.json yarn.lock pnpm-lock.yaml)
    [[ "$line" =~ ^([Cc][Oo][Pp][Yy]|[Aa][Dd][Dd])[[:space:]] ]] || return 1
    line=$(normalize_instruction_body "${line#* }")
    read -r -a toks <<< "$line"
    # The last token is the destination when COPY/ADD has somewhere to put it.
    if [ "${#toks[@]}" -ge 2 ]; then
        toks=("${toks[@]:0:${#toks[@]}-1}")
    fi
    for t in "${toks[@]}"; do
        if [ "$t" = "." ] || [ "$t" = "./" ]; then
            return 0
        fi
        base=${t##*/}
        [ -z "$base" ] && continue
        for name in "${names[@]}"; do
            # shellcheck disable=SC2053 # source basename is a glob on purpose
            if [[ "$name" == $base ]]; then
                return 0
            fi
        done
    done
    return 1
}

# 0 when a leading --mount on this raw RUN bind-mounts a node lockfile.
# type=bind, or no type= key (bind is the default), counts. The basename of
# source=, src=, target=, dst=, or destination= must be a lockfile name.
# Cache, secret, and tmpfs mounts do not. Callers must not keep the result
# for a later instruction.
run_mounts_node_lockfile() {
    local body="$1" t="" mount="" part="" key="" val="" base="" typ=""
    local hit_name=0
    local -a toks=() parts=()
    read -r -a toks <<< "$body"
    for t in "${toks[@]}"; do
        [[ "$t" == --* ]] || break
        [[ "$t" == --mount=* ]] || continue
        mount=${t#--mount=}
        typ=""
        hit_name=0
        IFS=',' read -r -a parts <<< "$mount"
        for part in "${parts[@]}"; do
            key=${part%%=*}
            val=${part#*=}
            case "$key" in
                type)
                    typ=$val
                    ;;
                source|src|target|dst|destination)
                    base=${val##*/}
                    case "$base" in
                        package-lock.json|npm-shrinkwrap.json|yarn.lock|pnpm-lock.yaml)
                            hit_name=1
                            ;;
                    esac
                    ;;
            esac
        done
        if [ "$hit_name" -eq 1 ] && { [ -z "$typ" ] || [ "$typ" = "bind" ]; }; then
            return 0
        fi
    done
    return 1
}

# $1 is a normalized RUN/ADD body; the rest are NAME=VALUE for ARGs whose
# default is a floating ref. Prints one reason per ARG used as a ref:
# @$V / @${V} (go install, git refs), :$V (image or tag), #$V (ADD git#ref),
# --branch $V / --branch=$V / -b $V, or $V as an argument anywhere in a git
# command (checkout, switch, fetch origin $V, reset --hard $V, ...). Callers
# pass the body with quotes removed (unquoted_body), so @"$V" and
# checkout "$V" match. Commands are split on && || ; | so $V in an echo next
# to a git command doesn't count.
floating_arg_refs() {
    local body="$1"
    shift
    local entry="" name="" val="" v="" re="" seg="" hit=0
    # Backtick is a regex atom; a variable keeps it out of command substitution.
    local git_word_re='[[:space:]/(`]git[[:space:]]'
    local segs=${body//&&/;}
    segs=${segs//||/;}
    segs=${segs//|/;}
    for entry in "$@"; do
        name=${entry%%=*}
        val=${entry#*=}
        v="[$][{]?${name}([}:]|[^A-Za-z0-9_]|$)"
        re="(@|:|#|--branch[=[:space:]]+|(^|[[:space:]])-b[[:space:]]*)${v}"
        hit=0
        if [[ "$body" =~ $re ]]; then
            hit=1
        else
            while IFS= read -r seg; do
                # '/' so origin/$V and refs/heads/$V match; '(' and backtick so $(git) does.
                if [[ " $seg " =~ $git_word_re ]] && [[ " $seg" =~ [[:space:]=/]${v} ]]; then
                    hit=1
                    break
                fi
            done <<< "${segs//;/$'\n'}"
        fi
        if [ "$hit" -eq 1 ]; then
            printf 'uses ARG %s, whose default is a floating ref (%s), as a ref\n' "$name" "$val"
        fi
    done
}

# Prints "<dash> <name>" (dash is - for <<-, + otherwise) for each heredoc BuildKit opens on this
# RUN/COPY/ADD line, in order. Like BuildKit's parser, the line is split into
# shell words (quotes and backslash escapes kept raw, so "use <<EOF syntax" is
# one word), and a word opens a heredoc only when it is [fd]<<[-]WORD with no
# further '<' (so <<<EOF, a here-string, doesn't) and it starts the word (so
# $((1<<SHIFT)), x<<EOF and $V<<EOF don't). The word is the rest with quotes
# and backslashes removed; ${X} in it stays literal, as in BuildKit. A '#'
# later in the line is not a comment to BuildKit, so
# "RUN x # <<EOF" does open a heredoc; its body is checked like any other.
heredoc_markers() {
    local s="$1"
    local w="" c="" q="" name="" dash=""
    local i=0 n=${#s} inword=0
    local -a words=()
    while [ "$i" -lt "$n" ]; do
        c=${s:$i:1}
        if [ -n "$q" ]; then
            w+="$c"
            if [ "$q" = '"' ] && [ "$c" = "\\" ] && [ $((i + 1)) -lt "$n" ]; then
                i=$((i + 1))
                w+="${s:$i:1}"
            elif [ "$c" = "$q" ]; then
                q=""
            fi
        elif [ "$c" = "\\" ]; then
            w+="$c"
            inword=1
            if [ $((i + 1)) -lt "$n" ]; then
                i=$((i + 1))
                w+="${s:$i:1}"
            fi
        elif [ "$c" = "'" ] || [ "$c" = '"' ]; then
            q=$c
            w+="$c"
            inword=1
        elif [[ "$c" == [[:space:]] ]]; then
            if [ "$inword" -eq 1 ]; then
                words+=("$w")
                w=""
                inword=0
            fi
        else
            w+="$c"
            inword=1
        fi
        i=$((i + 1))
    done
    if [ "$inword" -eq 1 ]; then
        words+=("$w")
    fi
    for w in "${words[@]}"; do
        if [[ "$w" =~ ^[0-9]*'<<'(-?)([^<]+)$ ]]; then
            dash=${BASH_REMATCH[1]}
            name=${BASH_REMATCH[2]}
            name=${name//\"/}
            name=${name//\'/}
            name=${name//\\/}
            [ -z "$name" ] && continue
            printf '%s %s\n' "${dash:-+}" "$name"
        fi
    done
}

cleanup_self_work() {
    if [ -n "${SELF_WORK}" ] && [ -d "${SELF_WORK}" ]; then
        rm -rf -- "${SELF_WORK}"
    fi
}
trap cleanup_self_work EXIT

# Join backslash continuations the way BuildKit does, so a checksum later in
# the same RUN is visible. After one trailing CR is stripped, a line continues
# when it matches a backslash followed only by spaces or tabs. That backslash
# and those trailing spaces or tabs are removed; leading whitespace on a
# continuation line is kept. A line that is only "\" opens a continuation.
# Comment lines and blank lines inside a continuation are dropped and the
# continuation carries on; whitespace may follow the backslash; a comment line
# never continues. Blank lines outside a continuation are printed as they are.
# BuildKit heredocs (RUN <<SH ... SH) are folded into their instruction: the
# body lines of a RUN heredoc are appended as "; line" so the RUN checks see
# every command in the script, and the bodies of COPY/ADD heredocs (file
# contents) are dropped so they aren't parsed as instructions. Comment lines
# inside a RUN heredoc are dropped so "# sha256sum -c" can't count as a check.
# A body ends only at a line equal to its word (leading tabs stripped for
# <<-), as in BuildKit. When no such line exists, UNTERMINATED_TAG is printed
# and nothing is skipped: the lines after the instruction are parsed again.
UNTERMINATED_TAG=$'\x01unterminated-heredoc'
emit_logical_lines() {
    local file="$1"
    local line="" acc="" logical="" folded="" word="" body="" cmp="" dash=""
    local i=0 n=0 is_run=0 t=0 start=0 found=0 cont=0
    local trimmed=""
    # Doubled \\ so the ERE engine matches a literal backslash, then spaces/tabs.
    local cont_re=$'^(.*)\\\\[ \t]*$'
    local -a lines=() terms=() dashes=()
    mapfile -t lines < "$file"
    n=${#lines[@]}
    while [ "$i" -lt "$n" ]; do
        line=${lines[$i]%$'\r'}
        i=$((i + 1))
        trimmed="${line#"${line%%[![:space:]]*}"}"
        if [ "$cont" -eq 1 ]; then
            # Drop the line. A comment here does not end the continuation,
            # even when that comment itself ends in a backslash.
            case "$trimmed" in
                ''|'#'*) continue ;;
            esac
        else
            case "$trimmed" in
                ''|'#'*)
                    printf '%s\n' "$line"
                    continue
                    ;;
            esac
        fi
        if [[ "$line" =~ $cont_re ]]; then
            acc+="${BASH_REMATCH[1]}"
            cont=1
            continue
        fi
        logical="${acc}${line}"
        acc=""
        cont=0
        terms=()
        dashes=()
        if [[ "$logical" == *'<<'* ]] \
            && [[ "$logical" =~ ^[[:space:]]*([Rr][Uu][Nn]|[Cc][Oo][Pp][Yy]|[Aa][Dd][Dd])[[:space:]] ]]; then
            is_run=0
            [[ "$logical" =~ ^[[:space:]]*[Rr][Uu][Nn][[:space:]] ]] && is_run=1
            while read -r dash word; do
                [ "$dash" = "-" ] || dash=""
                dashes+=("$dash")
                terms+=("$word")
            done < <(heredoc_markers "$logical")
        fi
        if [ "${#terms[@]}" -gt 0 ]; then
            start=$i
            folded=$logical
            for t in "${!terms[@]}"; do
                word=${terms[$t]}
                dash=${dashes[$t]}
                found=0
                while [ "$i" -lt "$n" ]; do
                    body=${lines[$i]%$'\r'}
                    i=$((i + 1))
                    cmp=$body
                    if [ -n "$dash" ]; then
                        cmp="${cmp#"${cmp%%[!$'\t']*}"}"
                    fi
                    if [ "$cmp" = "$word" ]; then
                        found=1
                        break
                    fi
                    if [ "$is_run" -eq 1 ]; then
                        body="${body#"${body%%[![:space:]]*}"}"
                        case "$body" in
                            ''|'#'*) ;;
                            *) folded+=" ; ${body%\\}" ;;
                        esac
                    fi
                done
                if [ "$found" -eq 0 ]; then
                    printf '%s %s\n' "$UNTERMINATED_TAG" "$word"
                    i=$start
                    folded=$logical
                    break
                fi
            done
            logical=$folded
        fi
        printf '%s\n' "$logical"
    done
    if [ "$cont" -eq 1 ]; then
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
    local argname="" entry=""
    local i=0 next=0 name_i=0 froms=0 status=0 is_from=0 allowed=0 node_lock=0 eff_lock=0
    local -a tokens=()
    local -a stages=()
    local -a floating_args=() kept=()

    rel="${file#"$root"/}"
    if [ ! -r "$file" ]; then
        echo "FAIL: cannot read ${rel}" >&2
        CHECK_FROMS=0
        return 1
    fi

    while IFS= read -r line || [ -n "$line" ]; do
        if [[ "$line" == "$UNTERMINATED_TAG "* ]]; then
            echo "FAIL: ${rel} has an unterminated heredoc (no line equal to ${line#"$UNTERMINATED_TAG "})" >&2
            status=1
            continue
        fi
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
            # A lockfile copied in an earlier stage does not apply here.
            node_lock=0
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
            argname=${BASH_REMATCH[1]}
            argval=${BASH_REMATCH[2]}
            argval=${argval//\"/}
            argval=${argval//\'/}
            argval=${argval%%[[:space:]]*}
            # A redeclared default replaces the earlier one.
            kept=()
            for entry in "${floating_args[@]}"; do
                [ "${entry%%=*}" = "$argname" ] || kept+=("$entry")
            done
            floating_args=("${kept[@]}")
            case "$(printf '%s' "$argval" | tr '[:upper:]' '[:lower:]')" in
                latest|master|main|head)
                    # Only a problem if it's used as a ref; see floating_arg_refs.
                    floating_args+=("${argname}=${argval}")
                    ;;
            esac
            continue
        fi

        if [[ "$trimmed" =~ ^[Rr][Uu][Nn][[:space:]]+(.*)$ ]]; then
            raw_body=${BASH_REMATCH[1]}
            body=$(normalize_instruction_body "$raw_body")
            if [[ "$body" =~ $FLOATING_REF_RE ]]; then
                echo "FAIL: ${rel} has a floating ref (@${BASH_REMATCH[1]}) in a RUN: ${trimmed}" >&2
                status=1
            fi
            if [[ "$body" =~ $DOWNLOAD_RE ]] && [[ ! "$body" =~ $CHECKSUM_RE ]]; then
                echo "FAIL: ${rel} downloads with wget or curl but the RUN does not run sha256sum -c: ${trimmed}" >&2
                status=1
            fi
            # A bind-mounted lockfile counts for this RUN only, not later ones.
            eff_lock=$node_lock
            if [ "$eff_lock" -eq 0 ] && run_mounts_node_lockfile "$raw_body"; then
                eff_lock=1
            fi
            while IFS= read -r reason; do
                [ -z "$reason" ] && continue
                echo "FAIL: ${rel} has an unpinned package install (${reason}): ${trimmed}" >&2
                status=1
            done < <(unpinned_installs "$body" "$eff_lock")
            while IFS= read -r reason; do
                [ -z "$reason" ] && continue
                echo "FAIL: ${rel} ${reason} in a RUN: ${trimmed}" >&2
                status=1
            done < <(floating_arg_refs "$(unquoted_body "$raw_body")" "${floating_args[@]}")
            continue
        fi

        if has_node_lockfile "$trimmed"; then
            node_lock=1
        fi

        if [[ "$trimmed" =~ ^[Aa][Dd][Dd][[:space:]]+(.*)$ ]]; then
            raw_body=${BASH_REMATCH[1]}
            body=$(normalize_instruction_body "$raw_body")
            while IFS= read -r reason; do
                [ -z "$reason" ] && continue
                echo "FAIL: ${rel} ${reason} in an ADD: ${trimmed}" >&2
                status=1
            done < <(floating_arg_refs "$(unquoted_body "$raw_body")" "${floating_args[@]}")
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
    # shellcheck disable=SC2016 # ${REF}/$GIT_REF/$V are Dockerfile text.
    reject_case arg-main-quoted "floating ref (main)" "ARG quoted main default used as --branch rejected" \
        'ARG REF="main"
RUN git clone --branch "${REF}" https://example.invalid/r.git /src'
    # shellcheck disable=SC2016 # ${REF}/$GIT_REF/$V are Dockerfile text.
    reject_case arg-head-tag "floating ref (HEAD)" "ARG HEAD default used as #ref in ADD rejected" \
        'ARG GIT_REF=HEAD
ADD https://example.invalid/r.git#$GIT_REF /src'
    # shellcheck disable=SC2016 # ${REF}/$GIT_REF/$V are Dockerfile text.
    reject_case arg-latest-heredoc "floating ref (latest)" "ARG latest used as @ref in a heredoc rejected" \
        'ARG V=latest
RUN <<SH
set -e
go install example.com/tool/cmd/tool@$V
SH'
    reject_case heredoc-latest "@latest" "go install @latest in a RUN heredoc rejected" \
        'RUN <<SH
go install example.com/t@latest
SH'
    reject_case heredoc-quoted-dash "@main" "go install @main in a quoted <<- heredoc rejected" \
        "RUN <<-'EOS'
	echo building
	go install example.com/t@main
	EOS"
    reject_case heredoc-dash-then-copy "COPY --from an unpinned image (alpine:3.18)" "lines after a <<- heredoc terminator are instructions again" \
        "RUN <<-EOS
	echo hi
	EOS
COPY --from=alpine:3.18 /etc/ssl /etc/ssl"
    reject_case heredoc-wget "sha256sum -c" "wget without a checksum in a RUN heredoc rejected" \
        'RUN <<SH
wget -q https://example.invalid/tool -O /tmp/tool
# sha256sum -c is only mentioned in this comment
chmod +x /tmp/tool
SH'
    reject_case heredoc-npm "node package install without an exact version: typescript" "npm install in a RUN heredoc rejected" \
        'RUN <<SH
npm install -g typescript
SH'
    reject_case heredoc-pip "pip install without ==version: requests" "pip install in a RUN heredoc rejected" \
        'RUN --mount=type=cache,target=/root/.cache <<SH
pip install requests
SH'
    reject_case heredoc-second "@HEAD" "the second heredoc of one RUN is checked" \
        'RUN <<A <<B
echo first
A
go install example.com/t@HEAD
B'
    reject_case continuation-latest "@latest" "go install @latest on a RUN continuation line rejected" \
        'RUN echo hi \
 && go install example.com/t@latest'
    reject_case cont-comment-from "unpinned FROM: FROM alpine:3.18" "comment inside a continuation opens no heredoc" \
        'RUN echo hi \
# see <<#c
 && echo there
FROM alpine:3.18
#c'
    reject_case cont-comment-add "ADDs a URL" "comment inside a continuation opens no heredoc around ADD" \
        'RUN echo hi \
# see <<#c
 && echo there
ADD https://example.invalid/t.tgz /tmp/
#c'
    reject_case cont-comment-copy "COPY --from an unpinned image (alpine:3.18)" "comment inside a continuation opens no heredoc around COPY" \
        'RUN echo hi \
# see <<#c
 && echo there
COPY --from=alpine:3.18 /etc/ssl /etc/ssl
#c'
    reject_case cont-comment-latest "@latest" "comment inside a continuation does not end it" \
        'RUN true && \
# c
 go install example.com/t@latest'
    reject_case cont-blank-latest "@latest" "blank line inside a continuation does not end it" \
        'RUN true && \

 go install example.com/t@latest'
    reject_case cont-bs-spaces "@latest" "spaces after a continuation backslash still continue" \
        "$(printf 'RUN true && \\  \n go install example.com/t@latest')"
    reject_case cont-sh-heredoc "@latest" "comment inside a continuation does not hide the joined heredoc" \
        'RUN sh \
# c
<<EOF
go install example.com/t@latest
EOF'
    reject_case cont-blank-wget "sha256sum -c" "blank line inside a continuation still joins the RUN" \
        'RUN true && \

 wget -q https://example.invalid/t -O /t'
    reject_case comment-no-continue "unpinned FROM: FROM alpine:3.18" "a comment ending in a backslash does not continue" \
        '# note \
FROM alpine:3.18'
    accept_case cont-comment-blank-ok "RUN continuation with an interior comment and blank line accepted" \
        "$(printf 'RUN wget -q https://example.invalid/t -O /t \\\n# verify\n\n && echo "%s  /t" | sha256sum -c -' "$pin")"
    # A '<<' that BuildKit doesn't treat as a heredoc must not hide the
    # instructions after it.
    reject_case heredoc-not-quoted "unpinned FROM: FROM alpine:3.18" "<<WORD inside quotes opens no heredoc" \
        'RUN echo "use <<EOF syntax"
FROM alpine:3.18
EOF'
    reject_case heredoc-escaped-dq "unpinned FROM: FROM alpine:3.18" "backslash escape inside double quotes opens no heredoc" \
        'RUN echo "a\" <<EOF"
FROM alpine:3.18
EOF'
    # shellcheck disable=SC2016 # $((...)) is Dockerfile text.
    reject_case heredoc-not-arith "unpinned FROM: FROM alpine:3.18" "1<<SHIFT in arithmetic opens no heredoc" \
        'RUN echo $((1<<SHIFT))
FROM alpine:3.18
SHIFT'
    reject_case heredoc-not-herestring "unpinned FROM: FROM alpine:3.18" "<<< here-string opens no heredoc" \
        'RUN cat <<<EOF
FROM alpine:3.18
EOF'
    reject_case heredoc-unterminated "unterminated heredoc (no line equal to EOF)" "unterminated heredoc rejected" \
        'RUN <<EOF
echo hi'
    reject_case heredoc-unterminated-rest "unpinned FROM: FROM alpine:3.18" "unterminated heredoc doesn't swallow the rest" \
        'RUN <<EOF
echo hi
FROM alpine:3.18'
    reject_case heredoc-trailing-space "unterminated heredoc (no line equal to EOF)" "terminator with trailing space doesn't end the heredoc" \
        'RUN <<EOF
echo hi
EOF 
FROM alpine:3.18'
    reject_case heredoc-comment-marker "@latest" "<<WORD after # still opens a heredoc (as in BuildKit)" \
        'RUN true # <<EOF
go install example.com/t@latest
EOF'
    reject_case heredoc-tab-eof "@latest" "plain <<EOF does not strip tabs from the terminator" \
        "$(printf 'RUN <<EOF\n\tEOF\ngo install example.com/t@latest\nEOF')"
    reject_case heredoc-terminator-lookalikes "@latest" "only a line equal to the word ends the heredoc" \
        'RUN <<EOF
echo EOF in the middle
EOFX
 EOF
go install example.com/t@latest
EOF'
    # shellcheck disable=SC2016 # $((...)) is Dockerfile text.
    accept_case heredoc-add-body "ADD and COPY heredoc bodies are file contents" \
        'ADD <<EOF /etc/notes.txt
FROM ubuntu:latest
RUN wget https://example.invalid/t
EOF
COPY <<-"EOT" /etc/more.txt
	EOT is not the end here
	RUN go install example.com/t@latest
	EOT
RUN echo $((1<<2)) "<<not-a-heredoc" && tr a b <<<abc'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-checkout "floating ref (main)" "ARG main used in git -C dir checkout rejected" \
        'ARG V=main
RUN git -C /s checkout ${V}'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-clone-b "floating ref (main)" "ARG main used as clone -b rejected" \
        'ARG V=main
RUN cd /tmp && git clone -b $V https://example.invalid/r.git /s'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-other-b "floating ref (main)" "ARG main used as -b of a non-git tool rejected" \
        'ARG V=main
RUN hg clone -b $V https://example.invalid/r /s'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-at-quoted "floating ref (master)" "ARG master used as @\"\$V\" rejected" \
        'ARG V=master
RUN go install example.com/t@"$V"'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-fetch "floating ref (main)" "ARG main used in git fetch origin rejected" \
        'ARG V=main
RUN git fetch origin $V'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-switch "floating ref (main)" "ARG main used in git switch rejected" \
        'ARG V=main
RUN git switch $V'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-reset "floating ref (HEAD)" "ARG HEAD used in git reset --hard rejected" \
        'ARG V=HEAD
RUN /usr/bin/git reset --hard ${V}'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-origin-reset "floating ref (main)" "ARG main used in git reset origin/ref rejected" \
        'ARG V=main
RUN git reset --hard origin/$V'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-origin-checkout "floating ref (main)" "ARG main used in git checkout origin/ref rejected" \
        'ARG V=main
RUN git checkout origin/${V}'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-refs-heads "floating ref (main)" "ARG main used in git fetch refs/heads rejected" \
        'ARG V=main
RUN git fetch origin refs/heads/$V'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-rev-parse "floating ref (main)" "ARG main used in git rev-parse origin/ref rejected" \
        'ARG V=main
RUN git checkout $(git rev-parse origin/$V)'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-rev-parse-assign "floating ref (main)" "ARG main used in an assigned git rev-parse rejected" \
        'ARG V=main
RUN X=$(git rev-parse origin/$V) && echo $X'
    # shellcheck disable=SC2016 # backticks and $V are Dockerfile text.
    reject_case arg-rev-parse-backtick "floating ref (main)" "ARG main used in a backtick git rev-parse rejected" \
        'ARG V=main
RUN X=`git rev-parse origin/$V` && echo $X'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-other-keeps "floating ref (main)" "redeclaring another ARG keeps a floating default" \
        'ARG V=main
ARG W=1
RUN git checkout $V'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-checkout-heredoc "floating ref (main)" "ARG main used in git checkout \"\$V\" in a heredoc rejected" \
        'ARG V=main
RUN <<SH
cd /s
git checkout "$V"
SH'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    reject_case arg-colon "floating ref (latest)" "ARG latest used as :\$TAG rejected" \
        'ARG TAG=latest
RUN crane export example.invalid/img:$TAG /tmp/fs.tar'
    # shellcheck disable=SC2016 # $V/${V}/$TAG are Dockerfile text.
    accept_case arg-redeclared "ARG redeclared to a pinned version accepted" \
        'ARG V=latest
ARG V=v1.2.3
RUN go install example.com/t@${V}'
    accept_case heredocs-ok "pinned heredoc RUN, COPY heredoc content and non-ref ARGs accepted" \
        "ARG BUILD_MODE=main
ARG CHANNEL=latest
ARG BRANCH=master
RUN echo \"mode=\$BUILD_MODE channel=\${CHANNEL}\" && make MODE=\${BUILD_MODE}
RUN git --version && echo \"\$BUILD_MODE\" | tee /tmp/mode
RUN <<SH
set -e
go install example.com/tool/cmd/tool@v1.2.3
wget -q https://example.invalid/tool -O /tmp/tool
echo \"${pin}  /tmp/tool\" | sha256sum -c -
SH
COPY <<EOF /etc/notes.txt
FROM ubuntu:latest
RUN go install example.com/t@latest
EOF
RUN echo done"
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
    reject_case npm-bare-nolock "bare npm install without a lockfile" "bare npm install without a lockfile rejected" \
        'RUN npm install'
    reject_case npm-bare-pkgjson "bare npm install without a lockfile" "package.json alone is not a lockfile" \
        'COPY package.json ./
RUN npm install --omit=dev'
    reject_case npm-bare-exec "bare npm install without a lockfile" "exec-form bare npm install rejected" \
        'RUN ["npm", "install"]'
    reject_case npm-bare-before-lock "bare npm install without a lockfile" "lockfile copied after the install does not count" \
        'RUN cd app && npm i
COPY package-lock.json ./'
    reject_case npm-bare-env-mention "bare npm install without a lockfile" "a lockfile named outside COPY/ADD does not count" \
        'ENV NPM_LOCK=package-lock.json
LABEL lockfile="yarn.lock" stage="deps"
RUN test -f package-lock.json || echo no lockfile
RUN npm install'
    reject_case yarn-bare "bare yarn install without a lockfile" "bare yarn rejected" \
        'RUN yarn --frozen-lockfile'
    reject_case pnpm-bare "bare pnpm install without a lockfile" "bare pnpm install rejected" \
        'RUN pnpm install'
    reject_case pip-bare-local "use -r/-c with --require-hashes, or --no-deps" "pip install . rejected" \
        'RUN pip install .'
    reject_case pip-bare-editable "bare pip install without a lockfile" "pip install -e ./app rejected" \
        'RUN python3 -m pip install --no-cache-dir -e ./app'
    reject_case pip-bare-wheel "bare pip install without a lockfile" "pip install of a local wheel rejected" \
        'RUN pip3 install /tmp/tool-1.0-py3-none-any.whl'
    reject_case lockfile-other-stage "bare npm install without a lockfile" "lockfile copied in an earlier stage does not count" \
        "COPY package-lock.json ./
FROM alpine:3.18@sha256:${pin}
COPY package.json ./
RUN npm install"
    reject_case lockfile-dest-only "bare npm install without a lockfile" "lockfile name only as a COPY destination does not count" \
        'COPY lock.json /app/package-lock.json
RUN npm install'
    reject_case lockfile-bind-scoped "bare npm install without a lockfile" "a bind-mounted lockfile counts only for that RUN" \
        'RUN --mount=type=bind,source=package-lock.json,target=package-lock.json true
RUN npm install'
    reject_case lockfile-cache-mount "bare npm install without a lockfile" "a cache mount is not a lockfile" \
        'RUN --mount=type=cache,target=/root/.npm/package-lock.json npm install'
    reject_case npm-prefix-bare "bare npm install without a lockfile" "npm --prefix install without a lockfile rejected" \
        'RUN npm --prefix /app install'
    reject_case npm-inst-bare "bare npm install without a lockfile" "npm inst without a lockfile rejected" \
        'RUN npm inst'
    reject_case yarn-global-unpinned "node package install without an exact version: esbuild" "yarn global add unpinned rejected" \
        'RUN yarn global add esbuild'
    reject_case npm-quoted-verb "bare npm install without a lockfile" "quoted npm install verb rejected" \
        "RUN npm 'install'"
    reject_case npm-escaped "bare npm install without a lockfile" "backslash inside the npm command rejected" \
        'RUN n\pm install'
    reject_case npm-path "bare npm install without a lockfile" "npm install by path rejected" \
        'RUN /usr/local/bin/npm install'
    reject_case pip-versioned-bare "bare pip install without a lockfile" "versioned pip install . rejected" \
        'RUN pip3.11 install .'
    reject_case pip-venv-bare "bare pip install without a lockfile" "pip install . by path rejected" \
        'RUN /opt/venv/bin/pip install .'
    reject_case pip-constraint-only "pip install -r/-c without --require-hashes" "pip -c without hashes rejected" \
        'RUN pip install -c constraints.txt .'
    accept_case lockfile-npm "bare npm install after a lockfile COPY accepted" \
        'COPY package.json package-lock.json ./
RUN npm install'
    accept_case lockfile-shrinkwrap "bare npm install after npm-shrinkwrap.json accepted" \
        'COPY npm-shrinkwrap.json ./
RUN npm install'
    accept_case lockfile-yarn "bare yarn install after a lockfile COPY accepted" \
        'COPY --chown=node:node yarn.lock ./
RUN yarn install --frozen-lockfile'
    accept_case lockfile-pnpm "bare pnpm install after a lockfile COPY accepted" \
        'COPY pnpm-lock.yaml ./
RUN pnpm i'
    accept_case lockfile-add "bare npm install after a lockfile ADD accepted" \
        'ADD package-lock.json /app/
RUN ["npm", "install"]'
    accept_case lockfile-glob "bare npm install after a lockfile glob COPY accepted" \
        'COPY package*.json ./
RUN npm install'
    accept_case lockfile-context "bare npm install after copying the build context accepted" \
        'COPY . .
RUN npm install'
    accept_case lockfile-bind "bare npm install with a bind-mounted lockfile accepted" \
        'RUN --mount=type=bind,source=package.json,target=package.json --mount=type=bind,source=package-lock.json,target=package-lock.json --mount=type=cache,target=/root/.npm npm install'
    accept_case lockfile-same-stage "lockfile copied in the same stage accepted" \
        "FROM alpine:3.18@sha256:${pin}
COPY package-lock.json ./
RUN npm install"
    accept_case node-no-install-nolock "non-install node commands accepted without a lockfile" \
        'RUN yarn build && yarn run test && npm run build && pnpm exec tsc --version
RUN npm install -g typescript@5.6.3
RUN npm ci'
    reject_case copy-from-image "COPY --from an unpinned image (alpine:3.18)" "COPY --from tag-only image rejected" \
        'COPY --from=alpine:3.18 /etc/ssl /etc/ssl'
    reject_case copy-from-space "COPY --from an unpinned image (golang:1.26)" "COPY --from <image> (space form) rejected" \
        'COPY --from golang:1.26 /usr/local/go /usr/local/go'
    accept_case pinned-installs "pinned installs, stage COPY and ADD checksum accepted" \
        "ARG TOOL_VERSION=v1.2.3
ARG BRANCH_NOTE=maintenance
RUN npm ci && npm install -g typescript@5.6.3 @scope/tool@1.0.0-rc.1
COPY web/package.json web/package-lock.json ./web/
RUN npm install
RUN cd web && npm run build
RUN pip install --no-cache-dir requests==2.32.3 -i https://pypi.org/simple
RUN pip install --require-hashes -r requirements.txt
RUN pip install --no-deps .
RUN pip install --require-hashes -c constraints.txt -e ./app
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
