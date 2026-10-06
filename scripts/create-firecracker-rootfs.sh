#!/bin/bash
# Create a bootable ext4 Firecracker rootfs .img from a .tar.gz export.
# Must run as root (NOPASSWD via sudoers for unattended builds).
set -euo pipefail

if [ "$#" -ne 3 ]; then
	echo "usage: $0 <tarball> <img_file> <size>" >&2
	echo "  example: $0 agent.img.tar.gz agent.img 512M" >&2
	exit 1
fi

tarball="$1"
img_file="$2"
size="$3"

if [ ! -f "$tarball" ]; then
	echo "tarball not found: $tarball" >&2
	exit 1
fi

# mkfs.ext4 is in /sbin or /usr/sbin. `sudo PATH=$PATH` drops those dirs.
mkfs_ext4="$(command -v mkfs.ext4 2>/dev/null || true)"
if [ -z "$mkfs_ext4" ] && [ -x /usr/sbin/mkfs.ext4 ]; then
	mkfs_ext4=/usr/sbin/mkfs.ext4
fi
if [ -z "$mkfs_ext4" ] && [ -x /sbin/mkfs.ext4 ]; then
	mkfs_ext4=/sbin/mkfs.ext4
fi
if [ -z "$mkfs_ext4" ]; then
	echo "mkfs.ext4 not found: install e2fsprogs (apt-get install e2fsprogs)" >&2
	exit 1
fi

owner_uid="${SUDO_UID:-$(id -u)}"
owner_gid="${SUDO_GID:-$(id -g)}"

if ! truncate -s "$size" "$img_file" 2>/dev/null; then
	count="${size%M}"
	dd if=/dev/zero of="$img_file" bs=1M count="$count" status=none
fi

"$mkfs_ext4" -F -L rootfs "$img_file" >/dev/null

mnt="$(mktemp -d)"
cleanup() {
	umount "$mnt" 2>/dev/null || true
	rmdir "$mnt" 2>/dev/null || true
}
trap cleanup EXIT

mount -o loop "$img_file" "$mnt"
tar -xzf "$tarball" -C "$mnt" --numeric-owner --same-owner 2>/dev/null \
	|| tar -xzf "$tarball" -C "$mnt" --numeric-owner
umount "$mnt"
rmdir "$mnt"
trap - EXIT

chown "$owner_uid:$owner_gid" "$img_file"
