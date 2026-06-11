#!/bin/bash
set -euo pipefail

# scripts/boot-metrics.sh
# Standalone parser for boot timing data (guest BOOT_TIMING lines in fc-*-console.log
# + host "BOOT host phase=..." lines in daemon log).
# Usage: scripts/boot-metrics.sh <vm-id>   or   make boot-metrics VM=...
#
# Looks for console in $AEGIS_STATE_DIR or ~/.aegis/state or /tmp/aegis .
# Also scans aegis.log in cwd for host phases.

VM="${1:-}"
if [[ -z "$VM" ]]; then
  echo "usage: $0 <vm-id e.g. agent-abc123 or memory-abc123 or court-persona-ciso>"
  echo "       (or: make boot-metrics VM=agent-foo)"
  echo ""
  echo "For collaboration model <1s work: run with AEGIS_BOOT_TIMING=1 (sudo -E ./bin/aegis start --foreground per AGENTS.md),"
  echo "trigger role agents (chat session for agent-/memory-, or Court on start), then collect for representative IDs."
  echo "See docs/implementation-plan/collaboration-model.md for tactics (pools, parallel, snapshot for Court, prebuilt .img)."
  exit 1
fi

STATE="${AEGIS_STATE_DIR:-}"
if [[ -z "$STATE" ]]; then
  if [[ -d "$HOME/.aegis/state" ]]; then
    STATE="$HOME/.aegis/state"
  elif [[ -d "/tmp/aegis" ]]; then
    STATE="/tmp/aegis"
  else
    STATE="$HOME/.aegis/state"
  fi
fi

CONSOLE="$STATE/fc-$VM-console.log"
DAEMON_LOG="aegis.log"
if [[ ! -f "$CONSOLE" && -f "$STATE/fc-$VM-console.log" ]]; then
  CONSOLE="$STATE/fc-$VM-console.log"
fi

echo "=== Boot metrics for $VM (parsed from console + daemon log) ==="
echo "State dir: $STATE"
echo "Console  : $CONSOLE"
echo ""

have_guest=0
if [[ -f "$CONSOLE" ]]; then
  echo "Guest phases (from BOOT_TIMING lines in console log):"
  # Extract phase + duration_ms, print table
  grep -E '^BOOT_TIMING:' "$CONSOLE" | while read -r line; do
    phase=$(echo "$line" | sed -n 's/.*phase=\([^ ]*\).*/\1/p')
    dur=$(echo "$line" | sed -n 's/.*duration_ms=\([0-9]*\).*/\1/p')
    if [[ -n "$phase" ]]; then
      printf "  %-35s %8s ms\n" "$phase" "${dur:-?}"
      have_guest=1
    fi
  done || true
  echo ""
else
  echo "(no console log found at $CONSOLE)"
fi

if [[ -f "$DAEMON_LOG" ]]; then
  echo "Host phases (from daemon log 'BOOT host phase=...'):"
  grep -E 'BOOT host phase=' "$DAEMON_LOG" | tail -30 | while read -r line; do
    echo "  $line"
  done || true
  echo ""
else
  echo "(no $DAEMON_LOG in cwd; host phases may be in journal or redirected output)"
fi

echo "Live/structured view (when daemon running): ./bin/aegis vm boot-metrics $VM"
echo "Raw console grep: cat $CONSOLE | grep -E '(BOOT_TIMING|register|ready|main_entry)' | tail -30"
echo ""
echo "Reminder: start with AEGIS_BOOT_TIMING=1 (sudo -E) to populate data."
