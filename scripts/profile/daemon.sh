#!/usr/bin/env bash
# Profiling daemon lifecycle for one arm.
# Exit: 0 ok, 2 usage, 3 already running, 4 not ready, 5 stop failed, 6 sudo needs a password.
# Stop only with `sudo -n ./bin/aegis stop`. Do not signal aegis or firecracker.

set -euo pipefail

PROFILE_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

usage() {
  echo "usage: daemon.sh start|stop|status|wait-ready <arm> [--out DIR]" >&2
  echo "DIR defaults to /tmp/aegis-profile/artifacts (pass the same DIR to start and stop)." >&2
}

if [[ $# -lt 2 ]]; then
  usage
  exit 2
fi

cmd=$1
arm=$2
shift 2
out_dir=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out)
      if [[ $# -lt 2 || -z "${2:-}" ]]; then
        echo "missing value for --out" >&2
        usage
        exit 2
      fi
      out_dir=$2
      shift 2
      ;;
    --out=*)
      out_dir=${1#--out=}
      if [[ -z "$out_dir" ]]; then
        echo "missing value for --out" >&2
        exit 2
      fi
      shift
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 2
      ;;
  esac
done

if [[ ! "$arm" =~ ^[A-Za-z0-9._-]+$ ]]; then
  echo "invalid arm name: $arm" >&2
  exit 2
fi

if ! command -v pgrep >/dev/null 2>&1; then
  echo "pgrep is required" >&2
  exit 2
fi

if [[ -z "$out_dir" ]]; then
  out_dir="/tmp/aegis-profile/artifacts"
fi
# Absolute, but do not create it until start. A bad arm must not leave a directory behind.
case "$out_dir" in
  /*) ;;
  *) out_dir="$PWD/$out_dir" ;;
esac

resolve_build_dir() {
  local err
  err=$(mktemp)
  if ! build_dir=$(PYTHONPATH="$PROFILE_DIR" python3 -c 'import sys
from common import arm_build_dir
print(arm_build_dir(sys.argv[1]))' "$arm" 2>"$err"); then
    echo "cannot resolve arm: $arm" >&2
    cat "$err" >&2
    rm -f "$err"
    exit 2
  fi
  rm -f "$err"
  if [[ -z "$build_dir" || ! -d "$build_dir" ]]; then
    echo "build dir missing for arm $arm: ${build_dir:-}" >&2
    exit 2
  fi
  if [[ ! -x "$build_dir/bin/aegis" ]]; then
    echo "missing executable $build_dir/bin/aegis" >&2
    exit 2
  fi
}

sudo_password_failed() {
  local exact=$1
  local output=$2
  if [[ "$output" == *"a password is required"* ]]; then
    printf '%s\n' "$exact"
    printf '%s\n' "$output"
    exit 6
  fi
}

running_snapshot() {
  # aegis.real is only here so a leftover wrapper process still counts as busy.
  # Anchor at argv0. A command line that only mentions the start command must not count.
  pgrep -af '^([^ ]*/)?(aegis|aegis\.real) start --foreground( |$)' 2>/dev/null || true
  pgrep -a -x firecracker 2>/dev/null || true
}

refuse_if_busy() {
  local aegis_ps fc_ps
  aegis_ps=$(pgrep -af '^([^ ]*/)?(aegis|aegis\.real) start --foreground( |$)' || true)
  fc_ps=$(pgrep -a -x firecracker || true)
  if [[ -n "$aegis_ps" || -n "$fc_ps" ]]; then
    echo "refusing to start: aegis or firecracker is already running" >&2
    if [[ -n "$aegis_ps" ]]; then
      printf '%s\n' "$aegis_ps" >&2
    fi
    if [[ -n "$fc_ps" ]]; then
      printf '%s\n' "$fc_ps" >&2
    fi
    exit 3
  fi
}

bounded() {
  # bounded <seconds> <cmd...> ; never lets a CLI poll block past the deadline.
  local seconds=$1
  shift
  if [[ "$seconds" -le 0 ]]; then
    return 124
  fi
  timeout "$seconds" "$@"
}

wait_ready() {
  local start now elapsed deadline last_status last_pools
  local daemon_ok infra_ok pools_ok health_ok
  local remaining slice
  start=$(date +%s)
  deadline=$((start + 180))
  last_status=""
  last_pools=""
  while true; do
    now=$(date +%s)
    elapsed=$((now - start))
    remaining=$((deadline - now))
    if [[ "$remaining" -le 0 ]]; then
      break
    fi
    slice=$remaining
    if [[ "$slice" -gt 15 ]]; then
      slice=15
    fi
    last_status=$(bounded "$slice" ./bin/aegis status 2>&1 || true)
    now=$(date +%s)
    remaining=$((deadline - now))
    if [[ "$remaining" -le 0 ]]; then
      break
    fi
    slice=$remaining
    if [[ "$slice" -gt 15 ]]; then
      slice=15
    fi
    last_pools=$(bounded "$slice" ./bin/aegis vm pools 2>&1 || true)
    now=$(date +%s)
    remaining=$((deadline - now))
    health_ok=0
    if [[ "$remaining" -gt 0 ]]; then
      slice=$remaining
      if [[ "$slice" -gt 5 ]]; then
        slice=5
      fi
      if curl -sf --max-time "$slice" http://localhost:8080/health >/dev/null 2>&1; then
        health_ok=1
      fi
    fi
    daemon_ok=0
    infra_ok=0
    pools_ok=0
    if [[ "$last_status" != *"daemon is not running"* && "$last_status" == *"daemon is running"* ]]; then
      daemon_ok=1
    fi
    # Status prints "Base infrastructure: ready (...)"; also accept the contract phrase.
    if grep -qiE 'base infrastructure:?[[:space:]]*ready' <<<"$last_status" \
      || grep -E 'Court personas online: 7([^0-9]|$)' <<<"$last_status" >/dev/null; then
      infra_ok=1
    fi
    if grep -qE 'agent-pooled|memory-pooled' <<<"$last_pools"; then
      pools_ok=1
    fi
    now=$(date +%s)
    elapsed=$((now - start))
    if [[ "$daemon_ok" -eq 1 && "$infra_ok" -eq 1 && "$pools_ok" -eq 1 && "$health_ok" -eq 1 ]]; then
      echo "ready in ${elapsed}s"
      return 0
    fi
    if [[ "$now" -ge "$deadline" ]]; then
      break
    fi
    sleep 2
  done
  elapsed=$(( $(date +%s) - start ))
  echo "wait-ready failed after ${elapsed}s" >&2
  echo "last status:" >&2
  printf '%s\n' "$last_status" >&2
  echo "last vm pools:" >&2
  printf '%s\n' "$last_pools" >&2
  return 4
}


invoking_home() {
  local home_dir="${HOME:-}"
  if [[ -z "$home_dir" || ! -d "$home_dir" ]]; then
    home_dir=$(getent passwd "$(id -un)" | cut -d: -f6)
  fi
  if [[ -z "$home_dir" || ! -d "$home_dir" ]]; then
    echo "cannot resolve home for profile.env" >&2
    exit 2
  fi
  printf '%s\n' "$home_dir"
}

# Absolute rootfs for this arm. An already-set AEGIS_ROOTFS_DIR wins so an
# operator can point one start at a directory without sudoers env_keep.
arm_rootfs_dir() {
  local home_dir=$1
  local rootfs leaf
  if [[ -n "${AEGIS_ROOTFS_DIR:-}" ]]; then
    rootfs=$AEGIS_ROOTFS_DIR
    if [[ -d "$rootfs" ]]; then
      rootfs=$(cd "$rootfs" && pwd)
    fi
  else
    case "$arm" in
      base) leaf=rootfs-base ;;
      A) leaf=rootfs-A ;;
      B) leaf=rootfs-B ;;
      *) leaf="rootfs-$arm" ;;
    esac
    rootfs="$home_dir/.aegis/firecracker/$leaf"
  fi
  case "$rootfs" in
    /*) ;;
    *) rootfs="$PWD/$rootfs" ;;
  esac
  printf '%s\n' "$rootfs"
}

sanitize_env_value() {
  local v=$1
  v=${v//$'\n'/}
  v=${v//$'\r'/}
  v=${v//\"/}
  printf '%s\n' "$v"
}

# sudo -n drops AEGIS_*. Write the invoking user's ~/.aegis/profile.env so the
# root daemon finds it via SUDO_USER. Do not wrap bin/aegis and do not export
# AEGIS_ENV_FILE through sudo.
write_profile_env() {
  local home_dir env_file rootfs model pm tmp line key
  home_dir=$(invoking_home)
  mkdir -p "$home_dir/.aegis"
  env_file="$home_dir/.aegis/profile.env"
  rootfs=$(sanitize_env_value "$(arm_rootfs_dir "$home_dir")")
  model=$(sanitize_env_value "${AEGIS_DEFAULT_MODEL:-qwen3-coder:30b}")
  pm=$(sanitize_env_value "${AEGIS_PM_MODEL:-qwen3.6:35b}")
  tmp=$(mktemp "$home_dir/.aegis/profile.env.tmp.XXXXXX")
  {
    echo "# Written by scripts/profile/daemon.sh for arm ${arm}."
    echo "# sudo -n drops AEGIS_*. The daemon loads unset keys from this file."
    printf 'AEGIS_COLLAB_TRACE=1\n'
    printf 'AEGIS_DEFAULT_MODEL="%s"\n' "$model"
    printf 'AEGIS_PM_MODEL="%s"\n' "$pm"
    printf 'AEGIS_ROOTFS_DIR="%s"\n' "$rootfs"
    if [[ -n "${AEGIS_KERNEL_PATH:-}" ]]; then
      printf 'AEGIS_KERNEL_PATH="%s"\n' "$(sanitize_env_value "$AEGIS_KERNEL_PATH")"
    fi
    if [[ -n "${AEGIS_BOOT_TIMING:-}" ]]; then
      printf 'AEGIS_BOOT_TIMING="%s"\n' "$(sanitize_env_value "$AEGIS_BOOT_TIMING")"
    fi
    if [[ -n "${AEGIS_DEBUG:-}" ]]; then
      printf 'AEGIS_DEBUG="%s"\n' "$(sanitize_env_value "$AEGIS_DEBUG")"
    fi
    if [[ -f "$env_file" ]]; then
      while IFS= read -r line || [[ -n "$line" ]]; do
        [[ "$line" =~ ^[[:space:]]*$ ]] && continue
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        key=${line%%=*}
        key=${key#export }
        key=${key//[[:space:]]/}
        case "$key" in
          AEGIS_COLLAB_TRACE|AEGIS_DEFAULT_MODEL|AEGIS_PM_MODEL|AEGIS_ROOTFS_DIR)
            continue
            ;;
          AEGIS_KERNEL_PATH)
            [[ -n "${AEGIS_KERNEL_PATH:-}" ]] && continue
            ;;
          AEGIS_BOOT_TIMING)
            [[ -n "${AEGIS_BOOT_TIMING:-}" ]] && continue
            ;;
          AEGIS_DEBUG)
            [[ -n "${AEGIS_DEBUG:-}" ]] && continue
            ;;
        esac
        if [[ "$key" =~ ^AEGIS_[A-Z0-9_]+$ ]]; then
          printf '%s\n' "$line"
        fi
      done <"$env_file"
    fi
  } >"$tmp"
  chmod 600 "$tmp"
  mv -f "$tmp" "$env_file"
  echo "daemon.sh: wrote $env_file (COLLAB_TRACE=1 DEFAULT_MODEL=$model PM_MODEL=$pm ROOTFS_DIR=$rootfs)"
}

# keep_alive 60m. A failed prewarm does not fail start.
prewarm_models() {
  local model pm name payload
  model=$(sanitize_env_value "${AEGIS_DEFAULT_MODEL:-qwen3-coder:30b}")
  pm=$(sanitize_env_value "${AEGIS_PM_MODEL:-qwen3.6:35b}")
  for name in "$model" "$pm"; do
    payload=$(printf '{"model":"%s","prompt":"ping","stream":false,"keep_alive":"60m"}' "$name")
    if curl -sS --max-time 600 -o /dev/null \
      -H 'Content-Type: application/json' \
      -X POST \
      --data "$payload" \
      http://127.0.0.1:11434/api/generate; then
      echo "prewarm ${name} ok"
    else
      echo "prewarm ${name} fail"
    fi
  done
  return 0
}

cmd_start() {
  refuse_if_busy
  cd "$build_dir"
  write_profile_env
  local arm_out log pidfile pid exact
  arm_out="$out_dir/$arm"
  mkdir -p "$arm_out"
  log="$arm_out/daemon.log"
  pidfile="$arm_out/daemon.pid"
  exact="sudo -n ./bin/aegis start --foreground"
  sudo -n ./bin/aegis start --foreground >"$log" 2>&1 &
  pid=$!
  echo "$pid" >"$pidfile"
  disown "$pid" 2>/dev/null || true
  if ! kill -0 "$pid" 2>/dev/null; then
    local output
    output=$(cat "$log" 2>/dev/null || true)
    sudo_password_failed "$exact" "$output"
  fi
  wait_ready
  prewarm_models
}

cmd_stop() {
  cd "$build_dir"
  local exact output start now deadline status fc elapsed remaining slice
  exact="sudo -n ./bin/aegis stop"
  output=$(sudo -n ./bin/aegis stop 2>&1 || true)
  sudo_password_failed "$exact" "$output"
  start=$(date +%s)
  deadline=$((start + 120))
  status=""
  while true; do
    now=$(date +%s)
    remaining=$((deadline - now))
    if [[ "$remaining" -le 0 ]]; then
      break
    fi
    slice=$remaining
    if [[ "$slice" -gt 15 ]]; then
      slice=15
    fi
    status=$(bounded "$slice" ./bin/aegis status 2>&1 || true)
    fc=$(pgrep -x firecracker || true)
    if [[ "$status" == *"daemon is not running"* && -z "$fc" ]]; then
      elapsed=$(( $(date +%s) - start ))
      echo "stopped in ${elapsed}s"
      rm -f "$out_dir/$arm/daemon.pid"
      return 0
    fi
    if [[ "$(date +%s)" -ge "$deadline" ]]; then
      break
    fi
    sleep 2
  done
  elapsed=$(( $(date +%s) - start ))
  echo "stop failed after ${elapsed}s" >&2
  printf '%s\n' "$status" >&2
  pgrep -a -x firecracker >&2 || true
  return 5
}

cmd_status() {
  cd "$build_dir"
  local status
  status=$(bounded 15 ./bin/aegis status 2>&1 || true)
  if [[ "$status" == *"daemon is not running"* ]]; then
    echo "not running"
  elif [[ "$status" == *"daemon is running"* ]]; then
    echo "running"
  else
    echo "not running"
  fi
  echo "pgrep -x aegis / aegis.real:"
  { pgrep -a -x aegis || true; pgrep -a -x aegis.real || true; } | sed '/^$/d' || true
  if ! pgrep -x aegis >/dev/null && ! pgrep -x aegis.real >/dev/null; then
    echo "(none)"
  fi
  echo "pgrep -x firecracker:"
  pgrep -a -x firecracker || echo "(none)"
}

resolve_build_dir

case "$cmd" in
  start)
    cmd_start
    ;;
  stop)
    cmd_stop
    ;;
  status)
    cmd_status
    ;;
  wait-ready)
    cd "$build_dir"
    wait_ready
    ;;
  *)
    usage
    exit 2
    ;;
esac
