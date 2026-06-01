#!/bin/bash
# Create an Aegis build directory and hand ownership back to the invoking user.
# Must run as root (NOPASSWD via sudoers for unattended builds).
set -euo pipefail

if [ "$#" -ne 1 ]; then
	echo "usage: $0 <directory>" >&2
	exit 1
fi

dir="$1"
owner_uid="${SUDO_UID:-$(id -u)}"
owner_gid="${SUDO_GID:-$(id -g)}"

mkdir -p "$dir"
chown "$owner_uid:$owner_gid" "$dir"
