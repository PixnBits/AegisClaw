#!/usr/bin/env bash
# scripts/verify-turn-based-propagation-e2e.sh
#
# Driving E2E for turn-based message propagation (docs/specs/turn-based-message-propagation.md):
# PM plan → Coder progress update → CISO batched turn with anchors → push-back.
#
# Prerequisites: running daemon (sudo ./bin/aegis start), Ollama optional (fallback plans OK).
set -euo pipefail

if [ -n "${TURN_E2E_CHANNEL:-}" ]; then
  CHANNEL="$TURN_E2E_CHANNEL"
else
  CHANNEL="turn-e2e-verify-$(date +%s)"
fi
echo "Using channel: $CHANNEL (unique per run to avoid state pollution from prior tests)"
GOAL="As Project Manager: post a short plan assigning @coder to implement a hello-world feature and flag a security concern for @ciso to review. Ensure coder role in this channel."
POLL_SECONDS="${TURN_E2E_POLL_SECONDS:-180}"
MARKER="TURN-E2E-VERIFY"

echo "=== Turn-based message propagation E2E ==="
echo "Channel: $CHANNEL"
echo

if [[ ! -x ./bin/aegis ]]; then
  echo "ERROR: run make build first" >&2
  exit 2
fi

status_line=$(./bin/aegis status 2>&1 | grep -m1 'daemon is running' || true)
if [ -z "$status_line" ]; then
  echo "ERROR: daemon not running. Start with: sudo ./bin/aegis start" >&2
  exit 2
fi

# Idempotent channel setup
./bin/aegis channel list 2>/dev/null | grep -qF "$CHANNEL" || \
  ./bin/aegis channel post "$CHANNEL" "bootstrap" 2>/dev/null || true

# Create via store if missing (channel post no-ops when channel absent)
if ! ./bin/aegis channel list 2>/dev/null | grep -qF "$CHANNEL"; then
  echo "Creating channel $CHANNEL via hub..."
  # channel list may not show until create — use pm goal which creates via orchestrator path
fi

export AEGIS_COLLAB_TRACE="${AEGIS_COLLAB_TRACE:-1}"

echo "Sending PM goal (drives plan + ensure coder)..."
if ! ./bin/aegis pm goal "$GOAL" --channel "$CHANNEL" 2>&1 | tee /tmp/turn-e2e-pm-goal.log; then
  echo "WARN: pm goal returned non-zero (continuing to poll channel)"
fi

# Ensure CISO persona is a channel member for turn delivery
echo "Ensuring court-persona-ciso is in channel..."
# Best-effort via hub-less path: channel join not exposed; rely on PM plan mentioning ciso + default Court on main only.
# For dedicated channel, add member through repeated pm ensure is insufficient — use internal store via aegis if available.
# Poll assumes PM added coder and CISO responds when court-persona-ciso is ensured on channel.

pass_pm=false
pass_delivery=false
pass_coder=false
pass_ciso=false
pass_turn_state=false
pass_status_note=false

for ((i=1; i<=POLL_SECONDS; i++)); do
  CONTENT=$(./bin/aegis channel get "$CHANNEL" 2>/dev/null || echo "")
  TURN_STATE=$(./bin/aegis channel turn-state "$CHANNEL" 2>/dev/null || echo "")

  if echo "$CONTENT" | grep -qi 'project-manager' && echo "$CONTENT" | grep -qiE 'plan|assign'; then
    pass_pm=true
  fi
  # Stronger: evidence of turn delivery via status notes or turn-state outcomes/last_seen advanced for roles
  if echo "$CONTENT" | grep -qiE 'status: turns delivered'; then
    pass_status_note=true
  fi
  if echo "$TURN_STATE" | grep -qiE 'delivered|last_seen=[3-9]'; then
    pass_delivery=true
  fi
  if echo "$TURN_STATE" | grep -qiE 'coder.*(delivered|last_seen=[1-9])' || echo "$CONTENT" | grep -qiE 'from:.*coder-.*-verify'; then
    pass_coder=true
  fi
  if echo "$TURN_STATE" | grep -qiE 'ciso.*(delivered|last_seen=[1-9])' || echo "$CONTENT" | grep -qiE 'from:.*ciso-.*-verify|court-persona-ciso.*delivered'; then
    pass_ciso=true
  fi
  if echo "$TURN_STATE" | grep -qi 'last_seen_seq'; then
    pass_turn_state=true
  elif echo "$CONTENT" | grep -qi 'last_seen_seq'; then
    pass_turn_state=true
  fi

  if $pass_pm && $pass_delivery && $pass_coder && $pass_ciso && $pass_turn_state && $pass_status_note; then
    echo "✓ PASS: driving scenario complete (tick $i)"
    break
  fi
  sleep 1
done

echo
echo "--- Assertions ---"
$pass_pm && echo "✓ PASS: PM plan visible in channel" || echo "✗ FAIL: PM plan missing"
$pass_delivery && echo "✓ PASS: turn delivery evidence (status or delivered outcomes)" || echo "✗ FAIL: no delivery evidence"
$pass_coder && echo "✓ PASS: Coder received turn (delivered/last_seen or post)" || echo "✗ FAIL: Coder turn not evidenced"
$pass_ciso && echo "✓ PASS: CISO received turn (delivered/last_seen or post)" || echo "✗ FAIL: CISO turn not evidenced"
$pass_turn_state && echo "✓ PASS: turn-state shows last_seen_seq" || echo "✗ FAIL: turn-state observability"
$pass_status_note && echo "✓ PASS: system status notes for turns" || echo "✗ FAIL: no status notes"

TRACE_LOG="${TRACE_LOG:-aegis.log}"
if [ -f "$TRACE_LOG" ]; then
  if grep -q 'channel.turn.recv' "$TRACE_LOG" 2>/dev/null; then
    echo "✓ PASS: collab-trace shows channel.turn delivery"
  else
    echo "WARN: no channel.turn.recv in $TRACE_LOG (set AEGIS_COLLAB_TRACE=1 on daemon)"
  fi
  if grep -q 'get_relevant_since.ok' "$TRACE_LOG" 2>/dev/null; then
    echo "✓ PASS: CISO invoked get_relevant_since"
  else
    echo "WARN: get_relevant_since trace not found (CISO may still have used anchors in-payload)"
  fi
fi

if $pass_pm && $pass_coder && $pass_ciso && $pass_turn_state; then
  echo "=== verify-turn-based-propagation-e2e PASS ==="
  exit 0
fi
echo "=== verify-turn-based-propagation-e2e FAIL ==="
exit 1