#!/usr/bin/env bash
# scripts/verify-portal-api-e2e.sh
#
# Contract + latency checks for Web Portal SPA APIs (host-intercepted on :8080).
# Fails on 404/500 or responses slower than AEGIS_PORTAL_API_MAX_MS (default 30000).
#
# Usage:
#   bash scripts/verify-portal-api-e2e.sh
#   make test-e2e-portal-api

set -euo pipefail

PORTAL_URL="${AEGIS_PORTAL_URL:-http://localhost:8080}"
MAX_MS="${AEGIS_PORTAL_API_MAX_MS:-30000}"

echo "=== AegisClaw portal API contract + latency E2E ==="
echo "Base: $PORTAL_URL (max ${MAX_MS}ms per request)"
echo

if ! curl -sf --max-time 5 "${PORTAL_URL}/health" >/dev/null 2>&1; then
  echo "ERROR: portal not reachable at ${PORTAL_URL}/health (start: sudo ./bin/aegis start)" >&2
  exit 2
fi

echo "Waiting for store-backed APIs (channel list)..."
ready_deadline=$((SECONDS + 120))
while (( SECONDS < ready_deadline )); do
  if curl -sf --max-time 5 -H 'Accept: application/json' "${PORTAL_URL}/api/channels" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

check_get() {
  local path=$1
  local label=$2
  local tmp
  tmp=$(mktemp)
  local metrics code elapsed_ms
  metrics=$(curl -sS -o "$tmp" -w '%{http_code} %{time_total}' --max-time $((MAX_MS / 1000 + 5)) \
    -H 'Accept: application/json' "${PORTAL_URL}${path}" || echo "000 999")
  code=$(echo "$metrics" | awk '{print $1}')
  elapsed_ms=$(python3 -c "print(int(float('$(echo "$metrics" | awk '{print $2}')') * 1000))")

  if [[ "$code" != "200" ]]; then
    echo "✗ ${label} HTTP ${code} (${elapsed_ms}ms) body=$(head -c 200 "$tmp")"
    rm -f "$tmp"
    return 1
  fi
  if ! python3 - "$tmp" <<'PY'
import json, sys
with open(sys.argv[1]) as f:
    json.load(f)
PY
  then
    echo "✗ ${label} invalid JSON (${elapsed_ms}ms)"
    rm -f "$tmp"
    return 1
  fi
  if (( elapsed_ms > MAX_MS )); then
    echo "✗ ${label} too slow: ${elapsed_ms}ms (max ${MAX_MS}ms)"
    rm -f "$tmp"
    return 1
  fi
  echo "✓ ${label} HTTP 200 in ${elapsed_ms}ms"
  rm -f "$tmp"
  return 0
}

check_post_channel() {
  local marker="PORTAL-API-E2E-${RANDOM}"
  local tmp post_code get_code elapsed_ms
  tmp=$(mktemp)
  post_code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time $((MAX_MS / 1000 + 5)) \
    -H 'Content-Type: application/json' -H 'Accept: application/json' \
    -d "{\"from\":\"user\",\"content\":\"${marker}: portal API post\"}" \
    -X POST "${PORTAL_URL}/api/channels/main" || echo "000")
  if [[ "$post_code" != "200" ]]; then
    echo "✗ POST /api/channels/main HTTP ${post_code}"
    return 1
  fi
  metrics=$(curl -sS -o "$tmp" -w '%{http_code} %{time_total}' --max-time $((MAX_MS / 1000 + 5)) \
    -H 'Accept: application/json' "${PORTAL_URL}/api/channels/main" || echo "000 999")
  get_code=$(echo "$metrics" | awk '{print $1}')
  elapsed_ms=$(python3 -c "print(int(float('$(echo "$metrics" | awk '{print $2}')') * 1000))")
  if [[ "$get_code" != "200" ]]; then
    echo "✗ GET /api/channels/main after post HTTP ${get_code}"
    rm -f "$tmp"
    return 1
  fi
  if ! grep -q "$marker" "$tmp"; then
    echo "✗ GET /api/channels/main missing posted message (${elapsed_ms}ms)"
    rm -f "$tmp"
    return 1
  fi
  if (( elapsed_ms > MAX_MS )); then
    echo "✗ GET /api/channels/main too slow: ${elapsed_ms}ms"
    rm -f "$tmp"
    return 1
  fi
  echo "✓ POST+GET /api/channels/main in ${elapsed_ms}ms"
  rm -f "$tmp"
  return 0
}

FAIL=0
check_get "/api/dashboard" "GET /api/dashboard" || FAIL=1
check_get "/api/monitoring" "GET /api/monitoring" || FAIL=1
check_get "/api/skills" "GET /api/skills" || FAIL=1
check_get "/api/proposals" "GET /api/proposals" || FAIL=1
check_get "/api/channels" "GET /api/channels" || FAIL=1
check_get "/api/channels/main" "GET /api/channels/main" || FAIL=1
check_post_channel || FAIL=1

# STOMP WebSocket endpoint (400 without upgrade = handler registered)
stomp_code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 5 "${PORTAL_URL}/stomp" || echo "000")
if [[ "$stomp_code" == "400" || "$stomp_code" == "426" || "$stomp_code" == "101" ]]; then
  echo "✓ /stomp endpoint reachable (HTTP ${stomp_code})"
else
  echo "✗ /stomp endpoint not reachable (HTTP ${stomp_code})"
  FAIL=1
fi

if [[ $FAIL -eq 0 ]]; then
  echo "Running STOMP WebSocket subscription check..."
  AEGIS_PORTAL_URL="$PORTAL_URL" AEGIS_STOMP_TIMEOUT_MS="${AEGIS_STOMP_TIMEOUT_MS:-20000}" \
    node scripts/check_portal_stomp.mjs || FAIL=1
fi

if [[ $FAIL -eq 0 ]]; then
  echo
  echo "=== E2E SUMMARY: PASS (portal SPA APIs within latency budget) ==="
  if [[ "${AEGIS_E2E_PORTAL_API_BROWSER:-1}" != "0" ]]; then
    export AEGIS_E2E_PORTAL_API_BROWSER=1
    echo "Running Playwright portal API spec..."
    bash scripts/run-playwright-e2e.sh e2e/portal-api.spec.js || FAIL=1
  fi
else
  echo
  echo "=== E2E SUMMARY: FAIL (portal API contract/latency) ==="
fi

exit $FAIL
