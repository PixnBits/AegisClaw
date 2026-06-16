#!/usr/bin/env bash
# scripts/verify-channel-portal-e2e.sh
#
# E2E: post a broadcast question via the Web Portal REST API (same path as the UI)
# and assert PM + 7 Court personas reply on the main channel.
#
# Prerequisites: running daemon (sudo ./bin/aegis start).
# Usage:
#   bash scripts/verify-channel-portal-e2e.sh
#   make test-e2e-portal-channel

set -euo pipefail

CHANNEL="${AEGIS_PORTAL_CHANNEL:-main}"
PORTAL_URL="${AEGIS_PORTAL_URL:-http://localhost:8080}"
MARKER="${AEGIS_PORTAL_E2E_MARKER:-PORTAL-E2E-VERIFY}"
POST_MSG="${AEGIS_PORTAL_E2E_MSG:-${MARKER}: Can you all tell me one improvement you would make if you had a magic wand?}"
POLL_SECONDS="${AEGIS_PORTAL_POLL_SECONDS:-420}"
POLL_INTERVAL=3
CHANNEL_JSON="${AEGIS_PORTAL_CHANNEL_JSON:-/tmp/aegis-portal-channel.json}"

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

echo "=== AegisClaw portal channel fan-out E2E (PM + 7 Court personas) ==="
echo "Channel: $CHANNEL via $PORTAL_URL"
echo

if [[ ! -x ./bin/aegis ]]; then
  echo "ERROR: ./bin/aegis not found. Run 'make build' first." >&2
  exit 2
fi

if ! ./bin/aegis status >/dev/null 2>&1; then
  echo "ERROR: daemon not running. Start with: sudo ./bin/aegis start" >&2
  exit 2
fi

if ! curl -sf --max-time 5 "${PORTAL_URL}/health" >/dev/null 2>&1; then
  echo "ERROR: portal not reachable at ${PORTAL_URL}/health" >&2
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
echo "Messages before portal post: $BEFORE_COUNT"

echo "Posting via portal API (from=operator, matches legacy portal default)..."
PORT_BODY=$(python3 - <<PY
import json
print(json.dumps({"from": "operator", "content": """${POST_MSG}"""}))
PY
)
HTTP_CODE=$(curl -sS -o /tmp/aegis-portal-post.json -w '%{http_code}' \
  -X POST "${PORTAL_URL}/api/channels/${CHANNEL}" \
  -H 'Content-Type: application/json' \
  -d "$PORT_BODY")
if [[ "$HTTP_CODE" != "200" ]]; then
  echo "ERROR: portal POST returned HTTP $HTTP_CODE: $(cat /tmp/aegis-portal-post.json 2>/dev/null)" >&2
  exit 2
fi
echo "Portal post accepted (HTTP 200)"

check_new_replies() {
  channel_messages_json
  python3 scripts/check_channel_portal_fanout.py "$CHANNEL_JSON" "$BEFORE_COUNT" "$MARKER" "${EXPECTED[@]}"
}

echo "Polling for ${#EXPECTED[@]} new agent replies (up to ${POLL_SECONDS}s)..."
deadline=$((SECONDS + POLL_SECONDS))
ASSERT_RC=0

while (( SECONDS < deadline )); do
  if check_new_replies >/dev/null 2>&1; then
    echo "✓ All expected members posted new replies after portal post"
    break
  fi
  missing_line=$(check_new_replies 2>/dev/null || true)
  echo "  waiting... ${missing_line:-still polling}"
  sleep "$POLL_INTERVAL"
done

if ! check_new_replies >/dev/null 2>&1; then
  echo "✗ FAIL: not all members responded within ${POLL_SECONDS}s"
  ASSERT_RC=1
fi

if [[ $ASSERT_RC -eq 0 ]]; then
  echo "=== E2E SUMMARY: PASS (portal channel fan-out — all agents responded) ==="
  if [[ "${AEGIS_E2E_PORTAL_BROWSER:-1}" != "0" ]]; then
    export AEGIS_E2E_PORTAL_BROWSER=1
    echo "Running Playwright portal UI verification..."
    bash scripts/run-playwright-e2e.sh e2e/channel-portal-collab.spec.js || ASSERT_RC=$?
  fi
else
  echo "=== E2E SUMMARY: FAIL (see output above) ==="
fi

exit $ASSERT_RC
