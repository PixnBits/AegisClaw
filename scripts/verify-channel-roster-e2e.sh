#!/usr/bin/env bash
# scripts/verify-channel-roster-e2e.sh
#
# E2E: post a roster intro question to the default "main" channel with all agents
# (Project Manager + 7 Court personas) and assert each member replies appropriately.
#
# Prerequisites: running daemon (sudo ./bin/aegis start), Ollama optional (fallback intros OK).
# Usage:
#   bash scripts/verify-channel-roster-e2e.sh
#   make test-e2e-roster

set -euo pipefail

CHANNEL="${AEGIS_ROSTER_CHANNEL:-main}"
INTRO_MSG='Can everyone tell me their name and a short description of what you do?'
POLL_SECONDS="${AEGIS_ROSTER_POLL_SECONDS:-420}"
POLL_INTERVAL=3

EXPECTED=(
  project-manager
  court-persona-ciso
  court-persona-security-architect
  court-persona-architect
  court-persona-senior-coder
  court-persona-tester
  court-persona-efficiency
  court-persona-user-advocate
)

declare -A KEYWORDS=(
  [project-manager]="project manager|coordinate|plan"
  [court-persona-ciso]="ciso|security officer|chief information security"
  [court-persona-security-architect]="security architect|attack surface"
  [court-persona-architect]="system architect|modularity|design"
  [court-persona-senior-coder]="senior coder|code quality|implementation"
  [court-persona-tester]="tester|testing strategy|coverage"
  [court-persona-efficiency]="efficiency|performance|resource"
  [court-persona-user-advocate]="user advocate|usability|accessibility"
)

echo "=== AegisClaw channel roster intro E2E (PM + 7 Court personas) ==="
echo "Channel: $CHANNEL"
echo

if [[ ! -x ./bin/aegis ]]; then
  echo "ERROR: ./bin/aegis not found. Run 'make build' first." >&2
  exit 2
fi

if ! ./bin/aegis status >/dev/null 2>&1; then
  echo "ERROR: daemon not running. Start with: sudo ./bin/aegis start" >&2
  exit 2
fi

echo "Waiting for Court personas (7) and collab readiness..."
ready_deadline=$((SECONDS + 120))
while (( SECONDS < ready_deadline )); do
  STATUS=$(./bin/aegis status 2>/dev/null || true)
  if echo "$STATUS" | grep -q 'Court personas online: 7' && echo "$STATUS" | grep -q 'Collab/PM/channels: ready'; then
    if ./bin/aegis --json channel get "$CHANNEL" >/dev/null 2>&1; then
      break
    fi
  fi
  sleep 2
done

COURT_LINE=$(./bin/aegis status 2>/dev/null | grep -i 'Court personas online' || true)
echo "${COURT_LINE:-Court personas online: unknown}"
if [[ -n "$COURT_LINE" ]] && echo "$COURT_LINE" | grep -qvE 'Court personas online: [7-9]|[1-9][0-9]+'; then
  echo "WARNING: fewer than 7 Court personas online; intros may be incomplete until Court is ready."
fi

wait_for_hub_registered() {
  local vm_id=$1
  local max_sec=${2:-120}
  local start=$SECONDS
  while (( SECONDS - start < max_sec )); do
    if ./bin/aegis vm logs "$vm_id" 2>/dev/null | grep -q 'registered as'; then
      return 0
    fi
    sleep 2
  done
  return 1
}

echo "Waiting for Court persona hub registration (sample: court-persona-ciso)..."
if ! wait_for_hub_registered "court-persona-ciso" 120; then
  echo "WARNING: court-persona-ciso not hub-registered yet; notify fan-out will retry"
fi

export AEGIS_CHANNEL_ACTIVITY_TIMEOUT="${AEGIS_CHANNEL_ACTIVITY_TIMEOUT:-180s}"
echo "Posting intro question (fan-out is automatic after store persist)..."
./bin/aegis channel post "$CHANNEL" "$INTRO_MSG"

CHANNEL_JSON="${AEGIS_ROSTER_CHANNEL_JSON:-/tmp/aegis-roster-channel.json}"

channel_messages_json() {
  ./bin/aegis --json channel get "$CHANNEL" 2>/dev/null >"$CHANNEL_JSON" || echo '{}' >"$CHANNEL_JSON"
}

check_all_responders() {
  channel_messages_json
  python3 scripts/check_channel_roster.py "$CHANNEL_JSON" "${EXPECTED[@]}"
}

echo "Polling channel for ${#EXPECTED[@]} member intros (up to ${POLL_SECONDS}s)..."
deadline=$((SECONDS + POLL_SECONDS))
ASSERT_RC=0

while (( SECONDS < deadline )); do
  if check_all_responders >/dev/null 2>&1; then
    echo "✓ All expected members responded with role-appropriate intros"
    break
  fi
  missing_line=$(check_all_responders 2>/dev/null || true)
  echo "  waiting... ${missing_line:-still polling}"
  sleep "$POLL_INTERVAL"
done

if ! check_all_responders >/dev/null 2>&1; then
  echo "✗ FAIL: not all members responded within ${POLL_SECONDS}s"
  ASSERT_RC=1
fi

echo
echo "=== Per-role verification ==="
channel_messages_json
AEGIS_ROSTER_CHECK_MODE=detail python3 scripts/check_channel_roster.py "$CHANNEL_JSON" "${EXPECTED[@]}"
PY_RC=$?
if [[ $PY_RC -ne 0 ]]; then
  ASSERT_RC=1
fi

if [[ $ASSERT_RC -eq 0 ]]; then
  echo "=== E2E SUMMARY: PASS (channel roster intro — all agents responded) ==="
else
  echo "=== E2E SUMMARY: FAIL (see per-role output above) ==="
fi

exit $ASSERT_RC
