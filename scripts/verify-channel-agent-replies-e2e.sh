#!/usr/bin/env bash
# scripts/verify-channel-agent-replies-e2e.sh
#
# Regression E2E: user post via portal → at least N agents channel.post to store.
# Catches hubclient decoder races (fan-out ok, zero store replies) and LLM pipeline breaks.
#
# Prerequisites: running daemon + Ollama reachable from network-boundary VM.
#
# Usage:
#   export AEGIS_DEFAULT_MODEL=gemma4:latest   # optional; or start with --default-model
#   sudo ./bin/aegis start
#   make test-e2e-channel-replies
#
# Optional trace (env only — not compiled into a separate binary):
#   export AEGIS_COLLAB_TRACE=1
#   sudo -E ./bin/aegis start --foreground 2>&1 | tee aegis.log

set -euo pipefail

CHANNEL="${AEGIS_PORTAL_CHANNEL:-main}"
PORTAL_URL="${AEGIS_PORTAL_URL:-http://localhost:8080}"
MARKER="${AEGIS_CHANNEL_REPLY_MARKER:-CHANNEL-REPLY-$(date +%s)}"
POST_MSG="${AEGIS_CHANNEL_REPLY_MSG:-${MARKER}: Please reply with one short sentence about how we should collaborate on this project.}"
MIN_REPLIES="${AEGIS_CHANNEL_MIN_AGENT_REPLIES:-2}"
MIN_COURT="${AEGIS_CHANNEL_MIN_COURT_REPLIES:-1}"
POLL_SECONDS="${AEGIS_CHANNEL_REPLY_POLL_SECONDS:-360}"
POLL_INTERVAL=3
CHANNEL_JSON="${AEGIS_CHANNEL_REPLY_JSON:-/tmp/aegis-channel-reply.json}"
DAEMON_LOG="${AEGIS_DAEMON_LOG:-aegis.log}"

echo "=== Channel agent reply regression E2E ==="
echo "Channel: $CHANNEL  Portal: $PORTAL_URL"
echo "Marker:  $MARKER"
echo "Need:    >= ${MIN_REPLIES} agent replies (>= ${MIN_COURT} court-persona-*) in store (non-canned)"
echo "Tip:     start daemon with --default-model gemma4:latest for faster/reliable LLM on dev hardware"
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
echo "Messages before post: $BEFORE_COUNT"

PORT_BODY=$(python3 - <<PY
import json
print(json.dumps({"from": "user", "content": """${POST_MSG}"""}))
PY
)
HTTP_CODE=$(curl -sS -o /tmp/aegis-channel-reply-post.json -w '%{http_code}' \
  -X POST "${PORTAL_URL}/api/channels/${CHANNEL}" \
  -H 'Content-Type: application/json' \
  -d "$PORT_BODY")
if [[ "$HTTP_CODE" != "200" ]]; then
  echo "ERROR: portal POST returned HTTP $HTTP_CODE: $(cat /tmp/aegis-channel-reply-post.json 2>/dev/null)" >&2
  exit 2
fi
echo "Portal post accepted (HTTP 200)"

check_replies() {
  channel_messages_json
  python3 scripts/check_channel_min_agent_replies.py "$CHANNEL_JSON" "$BEFORE_COUNT" "$MARKER" "$MIN_REPLIES" "$MIN_COURT"
}

echo "Polling store for >= ${MIN_REPLIES} agent replies (up to ${POLL_SECONDS}s)..."
deadline=$((SECONDS + POLL_SECONDS))
ASSERT_RC=0

while (( SECONDS < deadline )); do
  if out=$(check_replies 2>/dev/null); then
    echo "✓ $out"
    break
  fi
  out=$(check_replies 2>/dev/null || true)
  echo "  waiting... ${out:-still polling}"
  sleep "$POLL_INTERVAL"
done

if ! out=$(check_replies 2>/dev/null); then
  echo "✗ FAIL: $(check_replies 2>/dev/null || echo 'insufficient agent replies')"
  ASSERT_RC=1
fi

if [[ -f "$DAEMON_LOG" ]] && grep -q '\[collab-trace\]' "$DAEMON_LOG" 2>/dev/null; then
  echo
  echo "--- Agent channel.post routes (collab trace) ---"
  grep '\[collab-trace\]\[hub\]\[route\].*dest=store cmd=channel.post' "$DAEMON_LOG" | tail -20 || true
  agent_posts=$(grep -c 'src=court-persona-\|src=project-manager' <<<"$(grep 'dest=store cmd=channel.post' "$DAEMON_LOG" 2>/dev/null || true)" || true)
  echo "Agent channel.post lines in log: ${agent_posts:-0}"
fi

if [[ $ASSERT_RC -eq 0 ]] && [[ "${AEGIS_E2E_CHANNEL_REPLIES_BROWSER:-1}" != "0" ]]; then
  echo
  echo "Running Playwright STOMP agent delivery check..."
  export AEGIS_E2E_LIVE_STOMP=1
  export AEGIS_CHANNEL_REPLY_MARKER="$MARKER"
  if bash scripts/run-playwright-e2e.sh e2e/channel-agent-replies.spec.js; then
    echo "✓ Playwright channel-agent-replies passed"
  else
    echo "✗ Playwright channel-agent-replies failed (store had replies; STOMP path may be broken)"
    ASSERT_RC=1
  fi
fi

echo
if [[ $ASSERT_RC -eq 0 ]]; then
  echo "=== E2E SUMMARY: PASS (channel agent replies) ==="
else
  echo "=== E2E SUMMARY: FAIL ==="
  echo "Tips:"
  echo "  - Rebuild microVMs after agent changes: sudo make build-microvms"
  echo "  - Confirm Ollama: curl -s http://127.0.0.1:11434/api/tags"
  echo "  - Guest logs: ./bin/aegis vm logs court-persona-ciso | tail -30"
  echo "  - Optional trace: export AEGIS_COLLAB_TRACE=1 && sudo -E ./bin/aegis start --foreground 2>&1 | tee aegis.log"
fi

exit $ASSERT_RC
