#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${SCRIPT_DIR}/.."
LOGS_DIR="${REPO_ROOT}/logs/live-test-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$LOGS_DIR"

printf "This will reset your local AegisClaw state directories and run the live end-to-end test.\n"
printf "Directories to be removed:\n"
printf "  User:  \$HOME/.config/aegisclaw  \$HOME/.local/share/aegisclaw  \$HOME/.cache/aegisclaw\n"
printf "  Root:  /root/.config/aegisclaw   /root/.local/share/aegisclaw   /root/.cache/aegisclaw\n"
read -r -p "Continue? (y/N) " ans
if [[ "${ans:-N}" != "y" && "${ans:-N}" != "Y" ]]; then
  echo "Aborting. No changes made."
  exit 1
fi

echo "Stopping possible running AegisClaw services/processes (may request sudo)..."
# Try systemd stop then pkill as fallback
sudo systemctl stop aegisclaw.service 2>/dev/null || true
sudo pkill -f "aegisclaw" 2>/dev/null || true
sudo pkill -f "guest-agent" 2>/dev/null || true
sleep 1

# Remove user-state directories
for d in "$HOME/.config/aegisclaw" "$HOME/.local/share/aegisclaw" "$HOME/.cache/aegisclaw"; do
  if [ -e "$d" ]; then
    echo "Removing $d"
    rm -rf "$d"
  fi
done

# The test runs as root (sudo), so also reset root's state directories.
# Without this, the audit log and proposal store accumulate across runs.
ROOT_HOME="$(sudo sh -c 'echo $HOME')"
if [ "$ROOT_HOME" != "$HOME" ]; then
  for d in "$ROOT_HOME/.config/aegisclaw" "$ROOT_HOME/.local/share/aegisclaw" "$ROOT_HOME/.cache/aegisclaw"; do
    if [ -e "$d" ]; then
      echo "Removing $d (root state)"
      sudo rm -rf "$d"
    fi
  done
fi

# Prepare log files
TS="$(date -u +%Y%m%dT%H%M%SZ)"
TEST_LOG="$LOGS_DIR/test-first-skill-live-$TS.log"
AEGIS_LOG_SRC="$REPO_ROOT/aegisclaw.log"
AUX_LOG="$LOGS_DIR/aux-files-$TS.txt"

echo "Logs will be written to: $TEST_LOG"

echo "Starting TestFirstSkillTutorialLive (this may take up to 15 minutes). Output will stream to console and be saved." 
# Run the single test. Invoke `sudo` only for the test command and preserve
# the invoking user's PATH so `go` is found even when elevated.
(
  cd "$REPO_ROOT"
  sudo env PATH="$PATH" go test ./cmd/aegisclaw -run '^TestFirstSkillTutorialLive$' -timeout 20m -v -count=1
) |& tee "$TEST_LOG"
TEST_EXIT=${PIPESTATUS[0]:-0}

# Collect additional logs for analysis
if [ -f "$AEGIS_LOG_SRC" ]; then
  cp "$AEGIS_LOG_SRC" "$LOGS_DIR/aegisclaw.log.$TS"
fi

echo "Collecting system journal (last 500 lines) to $AUX_LOG (requires sudo)..."
if command -v journalctl >/dev/null 2>&1; then
  sudo journalctl -n 500 --no-pager 2>&1 | tee "$AUX_LOG" > /dev/null || true
else
  echo "journalctl not found; skipping system journal capture" > "$AUX_LOG"
fi

echo "Test finished with exit code: $TEST_EXIT"
echo "Logs and artifacts collected in: $LOGS_DIR"
exit "$TEST_EXIT"
