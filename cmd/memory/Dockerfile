# cmd/memory/Dockerfile
# Minimal container image for the Phase 1 Memory VM (paired 1:1 with an Agent Runtime).
#
# Exported by scripts/build-microvms-docker.sh into memory.img.tar.gz for Firecracker guests.
#
# SPEC REFERENCES:
#   - docs/specs/memory-vm.md (ShortTermContext 32k hard limit + zeroization, LongTermMemory
#     with TTL + semantic search + zeroization on purge, strict per-agent ACLs fail-closed)
#   - docs/specs/agent-runtime.md (1:1 pairing contract; agent calls memory.* exclusively via Hub)
#   - docs/prd/security-model.md (no cross-agent memory access; ACL enforcement before every op)
#   - no-stubs-plan/phase-1.md (real thin memory binary must be packageable for guests)

# === Build stage ===
FROM golang:1.24-alpine AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/memory ./cmd/memory
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /memory ./cmd/memory

# === Runtime stage ===
FROM alpine:3.18

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -S aegis && adduser -S -G aegis aegis

WORKDIR /app

COPY --from=builder /memory /usr/local/bin/memory

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

# Boot timing (opt-in via host AEGIS_BOOT_TIMING=1 + cmdline injection).
if grep -q 'aegis.boot_timing=1' /proc/cmdline 2>/dev/null || [ "${AEGIS_BOOT_TIMING:-0}" = "1" ]; then
  export AEGIS_BOOT_TIMING=1
  ts=$(date +%s 2>/dev/null || echo 0)000000000
  echo "BOOT_TIMING: phase=/init_boot_timing_enabled ts=${ts} duration_ms=0"
fi

ts=$(date +%s 2>/dev/null || echo 0)000000000
echo "BOOT_TIMING: phase=/init_start ts=${ts} duration_ms=0"

exec >/dev/console 2>&1
ts=$(date +%s 2>/dev/null || echo 0)000000000
echo "BOOT_TIMING: phase=/init_exec_binary ts=${ts} duration_ms=0"
echo "=== memory guest /init: entropy ready (via virtio-rng) ==="
exec /usr/local/bin/memory
EOF

RUN chmod +x /init

ENTRYPOINT ["/init"]

# Notes:
# - Implements hard-bounded short-term context + TTL long-term + explicit zeroization.
# - Enforces per-agent ACLs (one agent can never read another agent's memories).
# - Paired launch in orchestrator guarantees the 1:1 relationship required by the spec.