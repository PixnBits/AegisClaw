#!/usr/bin/env bash
# Full plan validation loop (requires daemon: make start).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "=== build binaries ==="
make build-binaries

echo "=== unit tests (hub/store/aegis) ==="
go test ./cmd/aegishub/... ./cmd/store/... ./cmd/aegis/... -count=1 -timeout 90s

echo "=== daemon status ==="
./bin/aegis status

echo "=== vm log error sweep ==="
make vm-logs-errors

echo "=== cURL chat harness ==="
make test-chat-curl

echo "=== Playwright chat E2E (live) ==="
make test-e2e

echo "=== hub-debug tail ==="
tail -30 /tmp/hub-debug.log 2>/dev/null || true

echo "PASS: validate-chat-sessions complete"
