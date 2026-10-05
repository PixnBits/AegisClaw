#!/usr/bin/env bash
# Unattended, resumable matrix driver for scripts/profile.
#
# Daemon lifecycle is delegated to daemon.sh. The only supported stop is
# `sudo -n ./bin/aegis stop` from the arm build directory (what daemon.sh stop
# runs). This script never calls pkill, never sends SIGKILL, and never signals
# a process named aegis or firecracker. On INT/TERM it SIGTERMs only the
# harness child it launched (run_one.py or daemon.sh), then calls daemon.sh stop.
set -euo pipefail
set -m

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

ARMS=""
SCENARIOS="all"
N="3"
OUT=""
PHASE="all"
DRY_RUN=0
NO_JUDGE=0
SHUFFLE_SEED=""

CURRENT_ARM=""
CURRENT_CHILD=""
STOPPING=0
ARM_RESTARTS=0
ARM_HEALTH_FAILS=0
RUN_FAILURES=0
INFRA_FAIL=0
SCORE_OK=0
LAST_RC=0
LAST_WALL=0
AEGIS_PIDS=""
FC_PIDS=""
SCENARIO_IDS=()
ARM_LIST=()
MATRIX_START=0

usage() {
  cat <<'EOF'
Usage: run_matrix.sh --arms base[,A,B] [--scenarios all|id,id] [--n 3]
                      [--out DIR] [--phase run|score|all] [--dry-run]
                      [--no-judge] [--shuffle-seed S]

Run each arm in order: daemon.sh start, then each scenario n=1..N (skip a
cell that already has result.json), then daemon.sh stop. Daemon stop is only
that call (sudo -n ./bin/aegis stop). This script never calls pkill and never
signals a process named aegis or firecracker. After every arm is
stopped, score and summarize (unless --phase run).

  --arms           Comma-separated arm ids, in run order. Required unless
                   --phase score.
  --scenarios      all (default) or comma-separated scenario ids.
  --n              Repeats per scenario (default 3). Cells are n1..nN.
  --out            Artifact root. Default:
                   /tmp/aegis-profile/artifacts/YYYYmmdd-HHMMSS
  --phase          run, score, or all (default all).
  --dry-run        Do not start or stop a daemon. Pass --dry-run to run_one.py.
  --no-judge       Call score.py without --judge.
  --shuffle-seed   Integer seed. Shuffles scenario order once; every arm uses
                   that same order. Repeat index n stays 1..N.

A run_one.py failure is logged and the matrix continues. The arm is aborted
only when the daemon health check fails twice (at most 2 restarts per arm).
Exit 0 if the requested phases finished (individual run failures included).
Exit 1 on lock contention, a health abort, a daemon left running, or a
score/summarize failure. Exit 2 on bad arguments.
EOF
}

die() {
  printf 'run_matrix.sh: %s\n' "$*" >&2
  exit 2
}

trim_ws() {
  local s=$1
  s=${s#"${s%%[![:space:]]*}"}
  s=${s%"${s##*[![:space:]]}"}
  printf '%s' "$s"
}

log() {
  local ts line
  ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  line="$ts $*"
  printf '%s\n' "$line"
  if [[ -n ${OUT:-} ]]; then
    printf '%s\n' "$line" >>"$OUT/matrix.log"
  fi
}

# Run a command, stream timestamped output to stdout and matrix.log.
# Always returns 0. Callers read LAST_RC and LAST_WALL.
# The child pid is recorded so a signal trap can SIGTERM that pid only.
run_logged() {
  local start
  log "CMD $*"
  start=$SECONDS
  set +e
  "$@" > >(TZ=UTC awk '{ print strftime("%Y-%m-%dT%H:%M:%SZ"), $0; fflush(); }' | tee -a "$OUT/matrix.log") 2>&1 &
  CURRENT_CHILD=$!
  # `if` keeps a non-zero child from firing ERR. A bare failing command still
  # fires ERR when `set +e` is on.
  if wait "$CURRENT_CHILD"; then
    LAST_RC=0
  else
    LAST_RC=$?
  fi
  CURRENT_CHILD=""
  # Reap the timestamp process substitution. It is not the daemon.
  wait 2>/dev/null || true
  set -e
  LAST_WALL=$((SECONDS - start))
  log "CMD rc=$LAST_RC wall_s=$LAST_WALL :: $*"
  return 0
}

snapshot_processes() {
  AEGIS_PIDS=$(pgrep -x aegis || true)
  FC_PIDS=$(pgrep -x firecracker || true)
  AEGIS_PIDS=${AEGIS_PIDS//$'\n'/ }
  FC_PIDS=${FC_PIDS//$'\n'/ }
  AEGIS_PIDS=$(trim_ws "$AEGIS_PIDS")
  FC_PIDS=$(trim_ws "$FC_PIDS")
}

processes_clear() {
  snapshot_processes
  log "pgrep -x aegis -> ${AEGIS_PIDS:-<none>}"
  log "pgrep -x firecracker -> ${FC_PIDS:-<none>}"
  [[ -z $AEGIS_PIDS && -z $FC_PIDS ]]
}

# Stop the current arm's daemon via daemon.sh only. Never signals aegis.
stop_current_arm() {
  local arm rc
  arm=${CURRENT_ARM:-}
  if [[ -z $arm || $DRY_RUN == 1 ]]; then
    CURRENT_ARM=""
    return 0
  fi
  if [[ $STOPPING == 1 ]]; then
    return 0
  fi
  STOPPING=1
  log "stopping arm $arm via daemon.sh stop"
  # `if` so a non-zero stop does not fire the ERR trap (it still fires under set +e).
  if bash "$SCRIPT_DIR/daemon.sh" stop "$arm" --out "$OUT" \
    > >(TZ=UTC awk '{ print strftime("%Y-%m-%dT%H:%M:%SZ"), $0; fflush(); }' | tee -a "$OUT/matrix.log") 2>&1; then
    rc=0
  else
    rc=$?
  fi
  if [[ $rc -ne 0 ]]; then
    log "WARN daemon.sh stop arm=$arm exit=$rc (daemon was not signalled)"
  fi
  CURRENT_ARM=""
  STOPPING=0
  return 0
}

# Invoked only from the INT/TERM/ERR traps (shellcheck does not see those calls).
# shellcheck disable=SC2317
terminate_harness_child() {
  local pid
  pid=${CURRENT_CHILD:-}
  if [[ $STOPPING == 1 || -z $pid ]]; then
    return 0
  fi
  if [[ -d /proc/$pid ]]; then
    # Direct child only (run_one.py or daemon.sh). Not its process group:
    # a group signal could hit the aegis daemon daemon.sh started.
    kill -TERM "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  CURRENT_CHILD=""
}

# shellcheck disable=SC2317
on_err() {
  local rc=$1
  local line=$2
  local cmd=${3:-}
  trap - ERR
  set +e
  log "ERROR line $line exit $rc cmd=$cmd; stopping current arm if any"
  terminate_harness_child
  stop_current_arm
  exit "$rc"
}

# shellcheck disable=SC2317
on_signal() {
  local sig=$1
  trap - INT TERM ERR
  set +e
  log "signal $sig; stopping current arm if any"
  terminate_harness_child
  stop_current_arm
  if [[ $sig == INT ]]; then
    exit 130
  fi
  exit 143
}

# shellcheck disable=SC2317
on_exit() {
  local rc=$?
  set +e
  stop_current_arm
  exit "$rc"
}

daemon_healthy() {
  local arm=$1
  local out rc
  if out=$(bash "$SCRIPT_DIR/daemon.sh" status "$arm" --out "$OUT" 2>&1); then
    rc=0
  else
    rc=$?
  fi
  log "daemon.sh status arm=$arm rc=$rc"
  if [[ -n $out ]]; then
    printf '%s\n' "$out" | TZ=UTC awk '{ print strftime("%Y-%m-%dT%H:%M:%SZ"), $0; fflush(); }' >>"$OUT/matrix.log"
  fi
  # Anchor on the status line. A later "not running" note must not override
  # an explicit running line, and the substring "running" inside "not running"
  # must not count as healthy.
  if printf '%s\n' "$out" | grep -Eiq '^[[:space:]]*(status:[[:space:]]*)?(daemon is not running|not running)\b'; then
    return 1
  fi
  if printf '%s\n' "$out" | grep -Eiq '^[[:space:]]*(status:[[:space:]]*)?(daemon is running|running)\b'; then
    return 0
  fi
  if printf '%s\n' "$out" | grep -iq 'not running'; then
    return 1
  fi
  if printf '%s\n' "$out" | grep -iq 'running'; then
    return 0
  fi
  if [[ $rc -ne 0 ]]; then
    return 1
  fi
  return 1
}

# Returns 0 if the arm may run another cell. Returns 1 if the arm must be
# aborted (health check failed twice). At most 2 restarts per arm; the
# initial start is not a restart.
ensure_daemon() {
  local arm=$1
  if [[ $DRY_RUN == 1 ]]; then
    return 0
  fi
  if daemon_healthy "$arm"; then
    ARM_HEALTH_FAILS=0
    return 0
  fi
  ARM_HEALTH_FAILS=$((ARM_HEALTH_FAILS + 1))
  log "daemon unhealthy arm=$arm health_fails=$ARM_HEALTH_FAILS restarts=$ARM_RESTARTS"
  if [[ $ARM_HEALTH_FAILS -ge 2 ]]; then
    log "abort arm $arm: daemon health check failed twice"
    return 1
  fi
  if [[ $ARM_RESTARTS -ge 2 ]]; then
    log "restart cap 2 reached for arm $arm; checking once more"
    if daemon_healthy "$arm"; then
      ARM_HEALTH_FAILS=0
      return 0
    fi
    log "abort arm $arm: daemon health check failed twice"
    return 1
  fi
  ARM_RESTARTS=$((ARM_RESTARTS + 1))
  log "restarting daemon arm=$arm restart=$ARM_RESTARTS/2"
  run_logged bash "$SCRIPT_DIR/daemon.sh" start "$arm" --out "$OUT"
  if [[ $LAST_RC -ne 0 ]]; then
    log "daemon restart exited $LAST_RC arm=$arm"
  fi
  if daemon_healthy "$arm"; then
    ARM_HEALTH_FAILS=0
    return 0
  fi
  log "abort arm $arm: daemon health check failed twice"
  return 1
}

discover_scenarios() {
  local f base part
  local -a files=() sorted=() parts=()
  SCENARIO_IDS=()
  if [[ $SCENARIOS == all ]]; then
    shopt -s nullglob
    files=("$SCRIPT_DIR/scenarios/"*.json)
    shopt -u nullglob
    if [[ ${#files[@]} -eq 0 ]]; then
      die "no scenario files in $SCRIPT_DIR/scenarios"
    fi
    mapfile -t sorted < <(printf '%s\n' "${files[@]}" | LC_ALL=C sort)
    for f in "${sorted[@]}"; do
      base=${f##*/}
      base=${base%.json}
      if [[ ! $base =~ ^[A-Za-z0-9_-]+$ ]]; then
        die "scenario id must match [A-Za-z0-9_-]+ ($base)"
      fi
      SCENARIO_IDS+=("$base")
    done
    return 0
  fi
  local raw=$SCENARIOS
  IFS=',' read -r -a parts <<<"$raw"
  if [[ ${#parts[@]} -eq 0 ]]; then
    die "--scenarios is empty"
  fi
  for part in "${parts[@]}"; do
    part=$(trim_ws "$part")
    if [[ -z $part ]]; then
      die "empty scenario id in --scenarios"
    fi
    if [[ ! $part =~ ^[A-Za-z0-9_-]+$ ]]; then
      die "scenario id must match [A-Za-z0-9_-]+ ($part)"
    fi
    if [[ ! -f $SCRIPT_DIR/scenarios/$part.json ]]; then
      die "missing scenario file: $SCRIPT_DIR/scenarios/$part.json"
    fi
    SCENARIO_IDS+=("$part")
  done
}

maybe_shuffle() {
  local line shuffled
  if [[ -z $SHUFFLE_SEED ]]; then
    return 0
  fi
  shuffled=$(python3 -c 'import random, sys
seed = int(sys.argv[1])
items = sys.argv[2:]
random.Random(seed).shuffle(items)
print("\n".join(items))' "$SHUFFLE_SEED" "${SCENARIO_IDS[@]}")
  SCENARIO_IDS=()
  while IFS= read -r line; do
    [[ -z $line ]] && continue
    SCENARIO_IDS+=("$line")
  done <<<"$shuffled"
}

parse_arms() {
  local part
  local -a parts=()
  ARM_LIST=()
  IFS=',' read -r -a parts <<<"$ARMS"
  if [[ ${#parts[@]} -eq 0 ]]; then
    die "--arms is empty"
  fi
  for part in "${parts[@]}"; do
    part=$(trim_ws "$part")
    if [[ -z $part ]]; then
      die "empty arm id in --arms"
    fi
    if [[ ! $part =~ ^[A-Za-z0-9_-]+$ ]]; then
      die "arm id must match [A-Za-z0-9_-]+ ($part)"
    fi
    ARM_LIST+=("$part")
  done
}

finish_arm() {
  local arm=$1
  if [[ $DRY_RUN == 1 ]]; then
    CURRENT_ARM=""
    return 0
  fi
  CURRENT_ARM=$arm
  stop_current_arm
  if processes_clear; then
    log "verified no aegis or firecracker after arm $arm"
    return 0
  fi
  log "processes still present after stop; second daemon.sh stop for arm $arm"
  CURRENT_ARM=$arm
  stop_current_arm
  if processes_clear; then
    log "verified no aegis or firecracker after second stop of arm $arm"
    return 0
  fi
  log "ERROR aegis or firecracker still running after daemon.sh stop. Not starting another arm and not scoring. Stop with: sudo -n ./bin/aegis stop (from that arm's build dir)."
  return 1
}

run_arm() {
  local arm=$1
  local sid k run_dir aborted=0
  CURRENT_ARM=$arm
  ARM_RESTARTS=0
  ARM_HEALTH_FAILS=0
  log "==== arm $arm ===="
  if [[ $DRY_RUN == 1 ]]; then
    log "dry-run: not starting daemon for arm $arm"
  else
    log "starting daemon for arm $arm"
    run_logged bash "$SCRIPT_DIR/daemon.sh" start "$arm" --out "$OUT"
    if [[ $LAST_RC -ne 0 ]]; then
      log "initial daemon start failed rc=$LAST_RC arm=$arm"
    fi
  fi

  for sid in "${SCENARIO_IDS[@]}"; do
    if [[ $aborted == 1 ]]; then
      break
    fi
    for ((k = 1; k <= N; k++)); do
      run_dir="$OUT/$arm/$sid/n$k"
      if [[ -f $run_dir/result.json ]]; then
        printf 'SKIP %s\n' "$run_dir"
        log "SKIP $run_dir"
        continue
      fi
      if ! ensure_daemon "$arm"; then
        aborted=1
        INFRA_FAIL=1
        log "skipping remaining cells for arm $arm"
        break
      fi
      log "run arm=$arm scenario=$sid n=$k"
      if [[ $DRY_RUN == 1 ]]; then
        run_logged python3 "$SCRIPT_DIR/run_one.py" \
          --arm "$arm" \
          --scenario "$SCRIPT_DIR/scenarios/$sid.json" \
          --n "$k" \
          --out "$OUT" \
          --dry-run
      else
        run_logged python3 "$SCRIPT_DIR/run_one.py" \
          --arm "$arm" \
          --scenario "$SCRIPT_DIR/scenarios/$sid.json" \
          --n "$k" \
          --out "$OUT"
      fi
      log "run wall_s=$LAST_WALL rc=$LAST_RC arm=$arm scenario=$sid n=$k"
      if [[ $LAST_RC -ne 0 ]]; then
        RUN_FAILURES=$((RUN_FAILURES + 1))
        log "run-level failure (matrix continues) arm=$arm scenario=$sid n=$k rc=$LAST_RC"
      fi
    done
  done

  if ! finish_arm "$arm"; then
    INFRA_FAIL=1
    return 1
  fi
  return 0
}

score_phase() {
  local -a score_cmd
  if ! processes_clear; then
    log "not scoring: aegis or firecracker is running. score.py --judge runs only after every daemon has stopped."
    INFRA_FAIL=1
    return 0
  fi
  log "scoring $OUT (daemons stopped)"
  score_cmd=(python3 "$SCRIPT_DIR/score.py" --out "$OUT")
  if [[ $NO_JUDGE == 0 ]]; then
    score_cmd+=(--judge)
  fi
  run_logged "${score_cmd[@]}"
  if [[ $LAST_RC -ne 0 ]]; then
    log "score.py failed rc=$LAST_RC"
    INFRA_FAIL=1
  else
    SCORE_OK=1
  fi
  run_logged python3 "$SCRIPT_DIR/summarize.py" \
    --runs "$OUT/runs_scored.jsonl" \
    --out "$OUT/summary.md"
  if [[ $LAST_RC -ne 0 ]]; then
    log "summarize.py failed rc=$LAST_RC"
    INFRA_FAIL=1
  fi
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case $1 in
      --arms | --scenarios | --n | --out | --phase | --shuffle-seed)
        if [[ $# -lt 2 || -z ${2:-} || $2 == --* ]]; then
          die "missing value for $1"
        fi
        case $1 in
          --arms) ARMS=$2 ;;
          --scenarios) SCENARIOS=$2 ;;
          --n) N=$2 ;;
          --out) OUT=$2 ;;
          --phase) PHASE=$2 ;;
          --shuffle-seed) SHUFFLE_SEED=$2 ;;
        esac
        shift 2
        ;;
      --dry-run)
        DRY_RUN=1
        shift
        ;;
      --no-judge)
        NO_JUDGE=1
        shift
        ;;
      -h | --help)
        usage
        exit 0
        ;;
      *)
        die "unknown argument: $1"
        ;;
    esac
  done

  case $PHASE in
    run | score | all) ;;
    *) die "invalid --phase: $PHASE (want run, score, or all)" ;;
  esac
  if [[ ! $N =~ ^[1-9][0-9]*$ ]]; then
    die "--n must be a positive integer"
  fi
  if [[ -n $SHUFFLE_SEED && ! $SHUFFLE_SEED =~ ^[0-9]+$ ]]; then
    die "--shuffle-seed must be a non-negative integer"
  fi
  SCENARIOS=$(trim_ws "$SCENARIOS")
  if [[ -z $SCENARIOS ]]; then
    die "--scenarios is empty"
  fi
}

require_file() {
  if [[ ! -f $1 ]]; then
    die "required file missing: $1"
  fi
}

acquire_lock() {
  # flock on this fd for the life of the process. Do not unlink the file:
  # a new inode would let a second matrix lock a different file.
  exec 9>>"$OUT/.matrix.lock"
  if ! flock -n 9; then
    printf 'run_matrix.sh: lock held: %s\n' "$OUT/.matrix.lock" >&2
    exit 1
  fi
  printf 'pid=%s started=%s\n' "$$" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >&9
}

main() {
  local arm elapsed total_line do_run do_score
  parse_args "$@"
  command -v flock >/dev/null 2>&1 || die "flock is required"
  command -v python3 >/dev/null 2>&1 || die "python3 is required"
  command -v pgrep >/dev/null 2>&1 || die "pgrep is required"

  do_run=0
  do_score=0
  if [[ $PHASE == run || $PHASE == all ]]; then
    do_run=1
  fi
  if [[ $PHASE == score || $PHASE == all ]]; then
    do_score=1
  fi

  if [[ $do_run == 1 ]]; then
    if [[ -z $ARMS ]]; then
      die "--arms is required unless --phase score"
    fi
    parse_arms
    discover_scenarios
    maybe_shuffle
    require_file "$SCRIPT_DIR/run_one.py"
    if [[ $DRY_RUN == 0 ]]; then
      require_file "$SCRIPT_DIR/daemon.sh"
    fi
  fi
  if [[ $do_score == 1 ]]; then
    require_file "$SCRIPT_DIR/score.py"
    require_file "$SCRIPT_DIR/summarize.py"
  fi

  if [[ -z $OUT ]]; then
    OUT=/tmp/aegis-profile/artifacts/$(date +%Y%m%d-%H%M%S)
  fi
  mkdir -p -- "$OUT" || die "cannot create $OUT"
  OUT=$(cd -- "$OUT" && pwd)

  MATRIX_START=$SECONDS
  acquire_lock
  trap 'on_err $? "$LINENO" "$BASH_COMMAND"' ERR
  trap 'on_signal INT' INT
  trap 'on_signal TERM' TERM
  trap 'on_exit' EXIT

  log "arms=${ARMS:-<none>} scenarios=$SCENARIOS n=$N phase=$PHASE dry_run=$DRY_RUN no_judge=$NO_JUDGE shuffle_seed=${SHUFFLE_SEED:-<none>} out=$OUT"
  if [[ $do_run == 1 ]]; then
    log "scenario order: ${SCENARIO_IDS[*]}"
  fi

  if [[ $do_run == 1 && $DRY_RUN == 0 ]]; then
    if ! processes_clear; then
      log "refusing to start: aegis or firecracker is already running. Stop it with sudo -n ./bin/aegis stop from that arm's build dir. This driver will not signal those processes."
      INFRA_FAIL=1
      do_run=0
      # A live daemon also blocks the judge.
      if [[ $do_score == 1 ]]; then
        log "not scoring while a daemon is running"
        do_score=0
      fi
    fi
  fi

  if [[ $do_run == 1 ]]; then
    for arm in "${ARM_LIST[@]}"; do
      if ! run_arm "$arm"; then
        log "stopping the arm loop: previous arm left aegis or firecracker running"
        break
      fi
    done
  fi

  if [[ $do_score == 1 ]]; then
    score_phase
  fi

  log "run_failures=$RUN_FAILURES infra_fail=$INFRA_FAIL"
  if [[ $SCORE_OK == 1 ]]; then
    printf 'sample list: %s\n' "$OUT/sample_list.md" >>"$OUT/matrix.log" || true
    printf 'sample list: %s\n' "$OUT/sample_list.md"
  fi
  elapsed=$((SECONDS - MATRIX_START))
  total_line="total elapsed: ${elapsed}s"
  printf '%s\n' "$total_line" >>"$OUT/matrix.log" || true
  printf '%s\n' "$total_line"

  if [[ $INFRA_FAIL -ne 0 ]]; then
    exit 1
  fi
  exit 0
}

main "$@"
