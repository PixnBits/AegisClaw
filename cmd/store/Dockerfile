# cmd/store/Dockerfile
# Minimal container image for the Store VM (persistent state, proposals, skill registry,
# audit log, timers, grants, git remote).
#
# Exported by scripts/build-microvms-docker.sh into store.img.tar.gz for Firecracker.
#
# SPEC REFERENCES:
#   - docs/specs/store-vm.md (single source of truth for proposals, skills, audit Merkle log,
#     persistent timers/grants, git operations)
#   - docs/specs/event-system.md (Store owns durable timers + grants)
#   - docs/prd/security-model.md + threat-model.md (no secrets here; only encrypted blobs for Boundary)
#   - host-daemon.md + no-stubs-plan (real Store binary must be bootable as guest)

# === Build stage ===
FROM golang:1.24-alpine AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

# Store needs substantial internal packages (crypto, events, etc.)
COPY cmd/store ./cmd/store
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /store ./cmd/store

# === Runtime stage ===
FROM alpine:3.18

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -S aegis && adduser -S -G aegis aegis

WORKDIR /app

COPY --from=builder /store /usr/local/bin/store

# Minimal init script for Firecracker boot.
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

# Boot timing (opt-in).
if grep -q 'aegis.boot_timing=1' /proc/cmdline 2>/dev/null || [ "${AEGIS_BOOT_TIMING:-0}" = "1" ]; then
  export AEGIS_BOOT_TIMING=1
  ts=$(date +%s 2>/dev/null || echo 0)000000000
  echo "BOOT_TIMING: phase=/init_boot_timing_enabled ts=${ts} duration_ms=0"
fi

exec >/dev/console 2>&1
ts=$(date +%s 2>/dev/null || echo 0)000000000
echo "BOOT_TIMING: phase=/init_start ts=${ts} duration_ms=0"
echo "=== store guest /init: entropy ready (via virtio-rng), starting store ==="
ts=$(date +%s 2>/dev/null || echo 0)000000000
echo "BOOT_TIMING: phase=/init_exec_binary ts=${ts} duration_ms=0"
exec /usr/local/bin/store
EOF

RUN chmod +x /init

# Use /init so Firecracker can boot this as a real microVM guest.
ENTRYPOINT ["/init"]
