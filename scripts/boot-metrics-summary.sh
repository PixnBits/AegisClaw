#!/usr/bin/env bash
# scripts/boot-metrics-summary.sh
# One-shot summary of boot metrics for base infrastructure + on-demand collab roles (AEGIS_BOOT_TIMING=1).
# When AEGIS_BOOT_METRICS_ASSERT=1, fails if host backend_start_return exceeds HOST_BUDGET_MS (default 2000)
# or guest register_complete exceeds GUEST_BUDGET_MS (default 2000).
set -euo pipefail

cd "$(dirname "$0")/.."

HOST_BUDGET_MS="${HOST_BUDGET_MS:-2000}"
GUEST_BUDGET_MS="${GUEST_BUDGET_MS:-2000}"
ASSERT="${AEGIS_BOOT_METRICS_ASSERT:-0}"
ASSERT_RC=0

echo "=== Boot-metrics summary (AEGIS_BOOT_TIMING=1) ==="
echo "Budgets: host backend_start_return <= ${HOST_BUDGET_MS}ms, guest register_complete <= ${GUEST_BUDGET_MS}ms (when assert=${ASSERT})"

metrics_for() {
  local id="$1"
  ./bin/aegis vm boot-metrics "$id" 2>/dev/null || bash scripts/boot-metrics.sh "$id" 2>/dev/null || true
}

parse_phase_ms() {
  local metrics="$1"
  local phase="$2"
  echo "$metrics" | awk -v p="$phase" '$1 == p { gsub(/[^0-9.]/, "", $2); print $2; exit }'
}

collect() {
  local id="$1"
  echo "--- $id ---"
  local metrics
  metrics=$(metrics_for "$id")
  if [[ -z "$metrics" ]] || echo "$metrics" | grep -q 'no metrics'; then
    echo "  (no metrics for $id; ensure AEGIS_BOOT_TIMING=1 and daemon was started with timing)"
    return 0
  fi
  echo "$metrics" | head -12

  if [[ "$ASSERT" != "1" ]]; then
    return 0
  fi

  local host_ms guest_ms
  host_ms=$(parse_phase_ms "$metrics" "host/backend_start_return")
  guest_ms=$(parse_phase_ms "$metrics" "guest/register_complete")

  if [[ -n "$host_ms" ]]; then
    if python3 -c "import sys; sys.exit(0 if float('${host_ms}') <= ${HOST_BUDGET_MS} else 1)"; then
      echo "  ✓ host/backend_start_return ${host_ms}ms <= ${HOST_BUDGET_MS}ms"
    else
      echo "  ✗ PERF: host/backend_start_return ${host_ms}ms exceeds budget ${HOST_BUDGET_MS}ms"
      ASSERT_RC=1
    fi
  fi
  if [[ -n "$guest_ms" ]]; then
    if python3 -c "import sys; sys.exit(0 if float('${guest_ms}') <= ${GUEST_BUDGET_MS} else 1)"; then
      echo "  ✓ guest/register_complete ${guest_ms}ms <= ${GUEST_BUDGET_MS}ms"
    else
      echo "  ✗ PERF: guest/register_complete ${guest_ms}ms exceeds budget ${GUEST_BUDGET_MS}ms"
      ASSERT_RC=1
    fi
  fi
}

for comp in store network-boundary web-portal court-persona-architect; do
  collect "$comp" || true
done

PM_ID=$(./bin/aegis vm list 2>/dev/null | grep project-manager | head -1 | awk '{print $1}' || true)
if [[ -n "$PM_ID" ]]; then
  collect "$PM_ID"
else
  echo "--- project-manager (on-demand) ---"
  echo "  (no ensured project-manager in vm list yet)"
fi

for role in coder tester; do
  ROLE_ID=$(./bin/aegis vm list 2>/dev/null | grep -E "^${role}-" | head -1 | awk '{print $1}' || true)
  if [[ -n "$ROLE_ID" ]]; then
    collect "$ROLE_ID"
  fi
done

echo "=== End boot-metrics summary ==="
exit $ASSERT_RC