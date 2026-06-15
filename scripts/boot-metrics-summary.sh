#!/usr/bin/env bash
# scripts/boot-metrics-summary.sh
# One-shot summary of boot metrics for base infrastructure + on-demand PM (when AEGIS_BOOT_TIMING=1).
set -euo pipefail

cd "$(dirname "$0")/.."

echo "=== Boot-metrics summary (AEGIS_BOOT_TIMING=1) ==="

collect() {
  local id="$1"
  echo "--- $id ---"
  if ./bin/aegis vm boot-metrics "$id" 2>/dev/null | head -6; then
    return 0
  fi
  if bash scripts/boot-metrics.sh "$id" 2>/dev/null | head -10; then
    return 0
  fi
  echo "  (no metrics for $id; ensure AEGIS_BOOT_TIMING=1 and daemon was started with timing)"
}

for comp in store network-boundary web-portal court-persona-architect; do
  collect "$comp" || true
done

PM_ID=$(./bin/aegis vm list 2>/dev/null | grep project-manager | head -1 | awk '{print $1}' || true)
if [ -n "$PM_ID" ]; then
  collect "$PM_ID"
else
  echo "--- project-manager (on-demand) ---"
  echo "  (no ensured project-manager in vm list yet)"
fi

echo "=== End boot-metrics summary ==="
