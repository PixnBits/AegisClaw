# cmd/court-scribe/Dockerfile
# Minimal container image for the Court Scribe microVM (lightweight coordination + audit clerk).
#
# This image is exported by scripts/build-microvms-docker.sh into court-scribe.img
# (and ultimately turned into a bootable Firecracker rootfs).
#
# SPEC REFERENCES (cited in every change):
#   - docs/specs/court-scribe.md §Purpose + §Core Principle (never sees or transmits proposal content; only coordination + state)
#   - docs/specs/court-scribe.md §Responsibilities (notify 7 personas by ID, collect signed votes, track state, notify proposer on complete)
#   - docs/specs/court-scribe.md §Security Requirements (all votes signed; malicious Scribe cannot forge/suppress; enforce all 7 before advance)
#   - docs/specs/governance-court.md §Architecture (Scribe as lightweight notification service; Court VMs pull directly from Store)
#   - docs/prd/governance-court.md (Court Scribe records structured summaries for the 7-persona board)
#   - docs/no-stubs-plan/phase-3.md 3.3 (full audit logging + exposure of real decisions)
#   - AGENTS.md (real thin binaries must be packageable and bootable as guests)
#
# The Scribe is a single instance. It forwards notifies to the 7 distinct court-persona-* sources
# (ACL wildcard supported) and aggregates only after all 7 signed votes arrive. It emits
# tamper-evident Merkle-signed decisions.

# === Build stage ===
FROM golang:1.24-alpine AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

# Need go.mod from repo root
COPY go.mod go.sum ./
RUN go mod download

# Copy only what the thin scribe needs
COPY cmd/court-scribe ./cmd/court-scribe
COPY internal ./internal

# Fully static binary (no CGO)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /court-scribe ./cmd/court-scribe

# === Runtime stage (very minimal) ===
FROM alpine:3.18

RUN apk add --no-cache ca-certificates tzdata

# Non-root (defense in depth)
RUN addgroup -S aegis && adduser -S -G aegis aegis
USER aegis

WORKDIR /app

COPY --from=builder /court-scribe /usr/local/bin/court-scribe

# Minimal init script for Firecracker boot.
# We explicitly redirect all output to the serial console so that
# our debug prints (and any crashes) appear in the host-captured
# fc-court-scribe-console.log.
COPY <<'EOF' /init
#!/bin/sh
export PATH=/usr/local/bin:/usr/bin:/bin
export HOME=/root

mount -t proc proc /proc 2>/dev/null || true
mount -t sysfs sys /sys 2>/dev/null || true
mount -t devtmpfs dev /dev 2>/dev/null || true

# Seed entropy early (historically to avoid crypto/rand blocking guest startup for ~60s).
# With the virtio-rng device now added to *every* Firecracker microVM
# (internal/sandbox/firecracker.go + GitHub #62) and a guest kernel with the
# virtio_rng driver (vmlinux-5.10+ from download script), the host supplies
# continuous entropy. CRNG inits in seconds ("crng init done" + virtio_rng attach
# early in dmesg) instead of 140s+. The early dd || true + threshold is kept as
# defense-in-depth (and for the web-portal's historical "start listeners before
# needing good rand" pattern, which is now unnecessary but harmless).
if [ -r /proc/sys/kernel/random/write_wakeup_threshold ]; then
  echo 1024 > /proc/sys/kernel/random/write_wakeup_threshold 2>/dev/null || true
fi
dd if=/dev/urandom of=/dev/null bs=32 count=1 2>/dev/null || true

# Force all stdout + stderr to the serial console.
# This is the most reliable way to get application output visible
# in Firecracker's captured console log for minimal guests.
exec >/dev/console 2>&1

# Boot timing instrumentation (gated by host AEGIS_BOOT_TIMING + cmdline).
if grep -q 'aegis.boot_timing=1' /proc/cmdline 2>/dev/null || [ "${AEGIS_BOOT_TIMING:-0}" = "1" ]; then
  export AEGIS_BOOT_TIMING=1
  ts=$(date +%s 2>/dev/null || echo 0)000000000
  echo "BOOT_TIMING: phase=/init_boot_timing_enabled ts=${ts} duration_ms=0"
fi

ts=$(date +%s 2>/dev/null || echo 0)000000000
echo "BOOT_TIMING: phase=/init_start ts=${ts} duration_ms=0"
echo "=== COURT-SCRIBE GUEST /init STARTING ==="
ts=$(date +%s 2>/dev/null || echo 0)000000000
echo "BOOT_TIMING: phase=/init_exec_binary ts=${ts} duration_ms=0"

exec /usr/local/bin/court-scribe
EOF

USER root
RUN chmod +x /init
USER aegis

# Use /init so the image can boot as a real Firecracker microVM.
ENTRYPOINT ["/init"]

# The thin main is the entrypoint.
# It expects:
#   - Distributed per-VM private key via AEGIS_VM_PRIVATE_KEY_PATH
#   - To register with AegisHub (unix or vsock:9999)
#   - Never to receive proposal content (strict guard)
#   - To emit full Merkle-signed decision records on review completion

# Notes:
# - This binary contains the complete real Court Scribe (no surface stubs).
# - Single instance per system. Works with the 7 court-persona-* instances.
# - When launched inside Firecracker, key hygiene and vsock path are identical to other guests.
# - Critical component (watchdog + orchestrator already list it).