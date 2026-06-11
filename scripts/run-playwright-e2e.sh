#!/bin/bash
# Run Playwright E2E in a real browser against the live daemon (sudo ./bin/aegis start).
# Optional AEGIS_E2E_FIXTURE=1 runs contract tests against the thin web-portal instead.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PLAYWRIGHT_IMAGE="${AEGIS_PLAYWRIGHT_IMAGE:-mcr.microsoft.com/playwright:v1.61.0-noble}"

require_daemon() {
	if [ -n "${AEGIS_E2E_FIXTURE:-}" ]; then
		return 0
	fi
	if curl -sf http://localhost:8080/health >/dev/null 2>&1; then
		return 0
	fi
	if [ -x "$ROOT/bin/aegis" ] && "$ROOT/bin/aegis" status 2>/dev/null | grep -q 'daemon is running'; then
		return 0
	fi
	echo "ERROR: daemon is not running on localhost:8080." >&2
	echo "Start the real system first: sudo ./bin/aegis start --foreground" >&2
	exit 1
}

run_docker() {
	if ! command -v docker >/dev/null 2>&1; then
		echo "Playwright browsers unavailable natively and docker not found." >&2
		exit 1
	fi
	require_daemon
	if [ -n "${AEGIS_E2E_FIXTURE:-}" ] && [ ! -x "$ROOT/bin/web-portal" ]; then
		echo "Building bin/web-portal for fixture-mode E2E in Docker..." >&2
		go build -o "$ROOT/bin/web-portal" ./cmd/web-portal
	fi
	echo "Running Playwright in Docker ($PLAYWRIGHT_IMAGE)..." >&2
	docker run --rm --network host \
		-v "$ROOT:$ROOT" \
		-w "$ROOT" \
		-e AEGIS_E2E_FIXTURE="${AEGIS_E2E_FIXTURE:-}" \
		-e AEGIS_E2E_USE_BIN_WEBPORTAL=1 \
		-e CI="${CI:-}" \
		"$PLAYWRIGHT_IMAGE" \
		bash -lc "npm install && npx playwright install chromium && npx playwright test $(printf '%q ' "$@")"
}

require_daemon

if [ -z "${AEGIS_FORCE_PLAYWRIGHT_DOCKER:-}" ] && command -v node >/dev/null 2>&1; then
	if [ ! -d node_modules/@playwright/test ]; then
		npm install
	fi
	if npx playwright install chromium; then
		exec npx playwright test "$@"
	fi
	echo "Native Playwright browser install failed; trying Docker..." >&2
fi

run_docker "$@"
