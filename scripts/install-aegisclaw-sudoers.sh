#!/usr/bin/env bash
# Install NOPASSWD rules so "make start" and microVM builds work without a password.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="${ROOT}/scripts/aegisclaw.sudoers"
DEST="/etc/sudoers.d/aegisclaw"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Re-run with sudo: sudo bash $0" >&2
  exit 1
fi

if ! visudo -cf "$SRC"; then
  echo "Refusing to install: sudoers syntax check failed for $SRC" >&2
  exit 1
fi

install -m 0440 "$SRC" "$DEST"
echo "Installed $DEST"
echo "Test: sudo -n ${ROOT}/bin/aegis status || sudo -n ${ROOT}/bin/aegis start"
