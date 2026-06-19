#!/usr/bin/env bash
# scripts/verify-channel-collab-trace-e2e.sh
#
# End-to-end channel collaboration trace: post via portal API, assert store replies,
# and print collab-trace stages from the daemon log when AEGIS_COLLAB_TRACE=1.
#
# Usage:
#   export AEGIS_COLLAB_TRACE=1
#   sudo -E ./bin/aegis start --foreground 2>&1 | tee aegis.log
#   # in another terminal:
#   bash scripts/verify-channel-collab-trace-e2e.sh
#
# Or with an existing log file:
#   AEGIS_DAEMON_LOG=aegis.log bash scripts/verify-channel-collab-trace-e2e.sh

set -euo pipefail

CHANNEL="${AEGIS_PORTAL_CHANNEL:-main}"
PORTAL_URL="${AEGIS_PORTAL_URL:-http://localhost:8080}"
MARKER="${AEGIS_COLLAB_MARKER:-COLLAB-TRACE-$(date +%s)}"
POST_MSG="${AEGIS_COLLAB_MSG:-${MARKER}: What is one concrete next step for this project?}"
POLL_SECONDS="${AEGIS_COLLAB_POLL_SECONDS:-180}"
POLL_INTERVAL=3
CHANNEL_JSON="${AEGIS_PORTAL_CHANNEL_JSON:-/tmp/aegis-collab-channel.json}"
DAEMON_LOG="${AEGIS_DAEMON_LOG:-aegis.log}"
MIN_REPLIES="${AEGIS_CHANNEL_MIN_AGENT_REPLIES:-2}"

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

echo "=== Channel collaboration trace E2E ==="
echo "Channel: $CHANNEL  Portal: $PORTAL_URL"
echo "Marker:  $MARKER"
echo

if [[ -f "$DAEMON_LOG" ]] && ! grep -q '\[collab-trace\]' "$DAEMON_LOG" 2>/dev/null; then
  echo "NOTE: No [collab-trace] lines in ${DAEMON_LOG} yet."
  echo "      Restart daemon with trace enabled, e.g.:"
  echo "        export AEGIS_COLLAB_TRACE=1 && sudo -E ./bin/aegis start --foreground 2>&1 | tee aegis.log"
  echo
fi

if [[ ! -x ./bin/aegis ]]; then
  echo "ERROR: ./bin/aegis not found. Run 'make build' first." >&2
  exit 2
fi

if ! ./bin/aegis status >/dev/null 2>&1; then
  echo "ERROR: daemon not running. Start with: AEGIS_COLLAB_TRACE=1 sudo -E ./bin/aegis start" >&2
  exit 2
fi

if ! curl -sf --max-time 5 "${PORTAL_URL}/health" >/dev/null 2>&1; then
  echo "ERROR: portal not reachable at ${PORTAL_URL}/health" >&2
  exit 2
fi

channel_messages_json() {
  ./bin/aegis --json channel get "$CHANNEL" 2>/dev/null >"$CHANNEL_JSON" || echo '{}' >"$CHANNEL_JSON"
}

message_count() {
  python3 - "$CHANNEL_JSON" <<'PY'
import json, sys
with open(sys.argv[1]) as f:
    data = json.load(f)
print(len(data.get("messages") or []))
PY
}

BEFORE_COUNT=$(channel_messages_json && message_count)
echo "Messages before post: $BEFORE_COUNT"

PORT_BODY=$(python3 - <<PY
import json
print(json.dumps({"from": "user", "content": """${POST_MSG}"""}))
PY
)
HTTP_CODE=$(curl -sS -o /tmp/aegis-collab-post.json -w '%{http_code}' \
  -X POST "${PORTAL_URL}/api/channels/${CHANNEL}" \
  -H 'Content-Type: application/json' \
  -d "$PORT_BODY")
if [[ "$HTTP_CODE" != "200" ]]; then
  echo "ERROR: portal POST returned HTTP $HTTP_CODE: $(cat /tmp/aegis-collab-post.json 2>/dev/null)" >&2
  exit 2
fi
echo "Portal post accepted (HTTP 200)"

check_min_replies() {
  channel_messages_json
  python3 scripts/check_channel_min_agent_replies.py "$CHANNEL_JSON" "$BEFORE_COUNT" "$MARKER" "$MIN_REPLIES" "${AEGIS_CHANNEL_MIN_COURT_REPLIES:-1}"
}

check_all_replies() {
  channel_messages_json
  python3 scripts/check_channel_portal_fanout.py "$CHANNEL_JSON" "$BEFORE_COUNT" "$MARKER" "${EXPECTED[@]}"
}

echo "Polling for >= ${MIN_REPLIES} agent replies (up to ${POLL_SECONDS}s)..."
deadline=$((SECONDS + POLL_SECONDS))
ASSERT_RC=0

while (( SECONDS < deadline )); do
  if check_min_replies >/dev/null 2>&1; then
    echo "✓ $(check_min_replies)"
    break
  fi
  line=$(check_min_replies 2>/dev/null || true)
  echo "  waiting... ${line:-still polling}"
  sleep "$POLL_INTERVAL"
done

if ! check_min_replies >/dev/null 2>&1; then
  echo "✗ FAIL: fewer than ${MIN_REPLIES} agent replies (hubclient/LLM pipeline broken)"
  check_min_replies || true
  ASSERT_RC=1
fi

if check_all_replies >/dev/null 2>&1; then
  echo "✓ All ${#EXPECTED[@]} expected agents replied (bonus)"
else
  echo "NOTE: not all agents replied ($(check_all_replies 2>/dev/null || true)) — NO_REPLY is valid"
fi

echo
echo "--- Collab trace stages (from ${DAEMON_LOG}) ---"
if [[ -f "$DAEMON_LOG" ]]; then
  grep '\[collab-trace\]' "$DAEMON_LOG" | tail -80 || echo "(no collab-trace lines — restart daemon with AEGIS_COLLAB_TRACE=1)"
  echo
  echo "Expected pipeline when healthy:"
  echo "  store.channel.post → store.channel.updated → daemon.channel.updated.recv"
  echo "  → daemon.fanout.* → hub.route (channel.activity) → agent.channel.activity.recv"
  echo "  → agent.channel.post.ok → store.channel.updated → daemon.stomp.notify.ok"
  echo "  → web-portal.stomp.notify.recv → web-portal.stomp.publish"
else
  echo "Log file not found. Capture with: AEGIS_COLLAB_TRACE=1 sudo -E ./bin/aegis start --foreground 2>&1 | tee aegis.log"
fi

echo
if [[ $ASSERT_RC -eq 0 ]]; then
  echo "=== E2E SUMMARY: PASS (channel collab trace) ==="
else
  echo "=== E2E SUMMARY: FAIL ==="
  echo "Tips:"
  echo "  - Enable trace: export AEGIS_COLLAB_TRACE=1 && sudo -E ./bin/aegis start --foreground 2>&1 | tee aegis.log"
  echo "  - Confirm Ollama/LLM reachable from network-boundary VM"
  echo "  - grep 'channel.reply.skip' / 'channel.post.fail' in guest logs"
  echo "  - Rebuild microVMs after agent changes: sudo make build-microvms"
fi

exit $ASSERT_RC
