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
  pgrep -a -x aegis 2>/dev/null || true
  pgrep -a -x firecracker 2>/dev/null || true
}

refuse_if_busy() {
  local aegis_ps fc_ps
  aegis_ps=$(pgrep -a -x aegis || true)
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

cmd_start() {
  refuse_if_busy
  cd "$build_dir"
  export AEGIS_COLLAB_TRACE=1
  export AEGIS_DEFAULT_MODEL="${AEGIS_DEFAULT_MODEL:-qwen3-coder:30b}"
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
  echo "pgrep -x aegis:"
  pgrep -a -x aegis || echo "(none)"
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
