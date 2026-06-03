# cmd/agent/Dockerfile
# Minimal container image for the Phase 1 Agent Runtime VM (thin 6-step loop binary).
#
# This image is exported by scripts/build-microvms-docker.sh into agent.img.tar.gz
# (and ultimately turned into a bootable Firecracker rootfs / agent.img).
#
# SPEC REFERENCES:
#   - docs/specs/agent-runtime.md (the real 6-step Observe→Think→Plan→Act→Execute→Judge loop
#     that must run inside Firecracker microVMs, talking only to AegisHub via vsock/JSON-RPC)
#   - docs/specs/memory-vm.md (1:1 paired with a Memory VM; context fetched at start of every turn)
#   - docs/prd/security-model.md (least-privilege guest, no direct component communication,
#     per-VM Ed25519 keys loaded from ephemeral 0600 file and zeroized)
#   - no-stubs-plan/phase-1.md (real thin binaries must be packageable and bootable as guests)

# === Build stage ===
FROM golang:1.24-alpine AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

# Need go.mod from repo root for the module
COPY go.mod go.sum ./
RUN go mod download

# Copy only what the thin agent needs (keeps image small and auditable)
COPY cmd/agent ./cmd/agent
COPY internal ./internal

# Fully static binary (no CGO) — required for minimal scratch/alpine rootfs in Firecracker
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /agent ./cmd/agent

# === Runtime stage (very minimal) ===
FROM alpine:3.18

RUN apk add --no-cache ca-certificates tzdata

# Non-root user exists for future hardening; /init runs the agent as root in the guest (matches store VM).
RUN addgroup -S aegis && adduser -S -G aegis aegis

WORKDIR /app

COPY --from=builder /agent /usr/local/bin/agent

# Minimal init for Firecracker (docker export drops ENTRYPOINT; kernel must use init=/init).
COPY <<'EOF' /init
#!/bin/sh
export PATH=/usr/local/bin:/usr/bin:/bin
export HOME=/root

mount -t proc proc /proc 2>/dev/null || true
mount -t sysfs sys /sys 2>/dev/null || true
mount -t devtmpfs dev /dev 2>/dev/null || true

if [ -r /proc/sys/kernel/random/write_wakeup_threshold ]; then
  echo 1024 > /proc/sys/kernel/random/write_wakeup_threshold 2>/dev/null || true
fi
# Entropy wait (defense-in-depth). With the virtio-rng device added to every
# Firecracker microVM (see internal/sandbox/firecracker.go + GitHub #62) and
# a guest kernel that includes the virtio_rng driver (vmlinux-5.10+ from
# scripts/download-firecracker-kernel.sh), the host VMM now supplies continuous
# entropy. CRNG initializes quickly ("random: crng init done" appears within
# seconds of boot, not 140s+), so this loop exits immediately. The dd + threshold
# logic is kept for non-FC paths and older kernels.
i=0
while [ "$i" -lt 60 ]; do
  if dd if=/dev/urandom of=/dev/null bs=32 count=1 2>/dev/null; then
    break
  fi
  i=$((i + 1))
  sleep 0.05
done

mkdir -p /etc/aegis 2>/dev/null || true

# Boot timing (opt-in): if host set AEGIS_BOOT_TIMING=1 then orchestrator injected
# aegis.boot_timing=1 on cmdline. Export so the Go binary's timing.IsEnabled sees env too.
if grep -q 'aegis.boot_timing=1' /proc/cmdline 2>/dev/null || [ "${AEGIS_BOOT_TIMING:-0}" = "1" ]; then
  export AEGIS_BOOT_TIMING=1
  ts=$(date +%s 2>/dev/null || echo 0)000000000
  echo "BOOT_TIMING: phase=/init_boot_timing_enabled ts=${ts} duration_ms=0"
fi

ts=$(date +%s 2>/dev/null || echo 0)000000000
echo "BOOT_TIMING: phase=/init_start ts=${ts} duration_ms=0"

exec >/dev/console 2>&1
echo "=== agent guest /init: entropy ready (via virtio-rng), key path=${AEGIS_VM_PRIVATE_KEY_PATH:-/etc/aegis/vmkey} hex_len=$(tr ' ' '\n' </proc/cmdline | grep -c vm_private_key_hex || true) ==="
ts=$(date +%s 2>/dev/null || echo 0)000000000
echo "BOOT_TIMING: phase=/init_exec_binary ts=${ts} duration_ms=0"
/usr/local/bin/agent || {
  echo "=== agent exited $?, holding guest open for logs ==="
  sleep 86400
}
EOF

RUN chmod +x /init

ENTRYPOINT ["/init"]

# Notes:
# - This binary contains the complete real Agent Runtime (no surface stubs).
# - When launched by the orchestrator's StartPairedAgentAndMemory, it receives its own
#   1:1 Memory VM peer and talks exclusively through AegisHub.
# - Future: add health/readiness endpoint if needed for the guest watchdog.