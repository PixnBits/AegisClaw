#!/usr/bin/env bash
# Fast regression for web-portal chat sessions (requires daemon on localhost:8080).
set -euo pipefail

BASE="${AEGIS_CHAT_BASE:-http://localhost:8080}"
CHAT_TIMEOUT="${AEGIS_CHAT_CURL_TIMEOUT:-120}"

if ! curl -sf "$BASE/health" >/dev/null 2>&1; then
  echo "ERROR: no daemon on $BASE (run: make start)" >&2
  exit 1
fi

echo "== GET /api/chat/sessions =="
COUNT=$(curl -sS "$BASE/api/chat/sessions" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d.get('sessions') or []))")
echo "sessions: $COUNT"

echo "== POST /api/chat/sessions =="
SID=$(curl -sS -X POST "$BASE/api/chat/sessions" \
  -H 'Content-Type: application/json' \
  -d "{\"title\":\"curl-debug-$(date +%s)\"}" \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['session']['id'])")
echo "created session: $SID"

echo "== GET /api/chat/history =="
curl -sS "$BASE/api/chat/history?session_id=$SID" | python3 -c "import sys,json; d=json.load(sys.stdin); print('history ok:', d.get('session',{}).get('id'))"

echo "== POST /chat/send?stream=1 (timeout ${CHAT_TIMEOUT}s) =="
set +e
OUT=$(curl -N --max-time "$CHAT_TIMEOUT" -X POST "$BASE/chat/send?stream=1" \
  -H 'Content-Type: application/json' \
  -H 'Accept: text/event-stream' \
  -d "{\"input\":\"curl ping $(date +%s)\",\"session_id\":\"$SID\"}" 2>&1)
RC=$?
set -e
echo "$OUT" | head -20
if echo "$OUT" | grep -qE 'thought_delta|content_delta|thought_event|tool_event|"type":"done"'; then
  echo "PASS: chat stream progressed beyond start"
  exit 0
fi
if [ "$RC" -eq 28 ]; then
  echo "FAIL: curl timed out — only start event or no progress (ensure agent-$SID is running: ./bin/aegis status)" >&2
  exit 1
fi
if echo "$OUT" | grep -q '"type":"start"'; then
  echo "FAIL: received start but no progress events" >&2
  exit 1
fi
echo "FAIL: unexpected chat response (rc=$RC)" >&2
exit 1
