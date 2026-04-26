#!/bin/bash
# cleanup-stale-builder.sh
# Cleans up stale builder VM tap devices and processes left from crashed daemon instances

set -e

echo "Cleaning up stale builder resources..."

# Find and delete all fc-builder-* or fc-builder- tap devices
for tap in $(ip link show | grep -oP 'fc-builder(?:-[a-f0-9]+)?'); do
    echo "Removing stale tap device: $tap"
    sudo ip link delete "$tap" 2>/dev/null || true
done

# Find and kill any orphaned firecracker processes for builder VMs
for pid in $(ps aux | grep '[f]irecracker.*builder-' | awk '{print $2}'); do
    echo "Killing orphaned builder firecracker process: $pid"
    sudo kill -9 "$pid" 2>/dev/null || true
done

# Clean up jailer chroot directories for builder VMs
if [ -d "/var/lib/aegisclaw/firecracker" ]; then
    for dir in /var/lib/aegisclaw/firecracker/builder-*; do
        if [ -d "$dir" ]; then
            echo "Removing stale chroot: $dir"
            sudo rm -rf "$dir" 2>/dev/null || true
        fi
    done
fi

echo "Cleanup complete"
