# cmd/court-persona/Dockerfile
# Minimal container image for the 7 Governance Court Persona microVMs (one instance per persona).
#
# This image is exported by scripts/build-microvms-docker.sh into court-persona.img
# (and ultimately turned into a bootable Firecracker rootfs).
#
# SPEC REFERENCES (cited in every change):
#   - docs/specs/governance-court.md §Architecture (7 independent Firecracker microVMs, never communicate directly)
#   - docs/specs/governance-court.md §Output Format Requirements (strict vote/reasoning/specific_feedback)
#   - docs/specs/governance-court.md §Implementation Guidance + §Test Requirements (persona-specific prompts, Abstain on uncertainty, distinguishable feedback, pull content from Store only)
#   - docs/specs/court-scribe.md §Communication Flow (Court VMs notified by ID only; independently pull proposal from Store; submit signed votes to Scribe)
#   - docs/specs/court-scribe.md §Security Requirements (all votes cryptographically signed; compromised VM cannot forge others)
#   - docs/prd/governance-court.md (7 personas, isolated microVMs, Court as central gate)
#   - docs/no-stubs-plan/phase-3.md 3.1 (real 7-persona Court microVMs, no simulation)
#   - AGENTS.md (real thin binaries must be packageable and bootable as guests; verification discipline)
#
# The same image is used for all 7 personas (ciso, security-architect, architect, senior-coder, tester, efficiency, user-advocate).
# The specific persona is selected at launch via:
#   - --persona flag (host/dev testing)
#   - AEGIS_COURT_PERSONA env var (orchestrator-injected for real Firecracker VMs)
#   - Kernel cmdline "aegis.persona=..." fallback (parsed in main)
#
# Each persona runs its own dedicated LLM prompt (via network-boundary llm.call) and produces
# cryptographically signed votes. No proposal content ever flows through the Court Scribe.

# === Build stage ===
FROM golang:1.24-alpine AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

# Need go.mod from repo root for the module (same as agent/memory)
COPY go.mod go.sum ./
RUN go mod download

# Copy only what the thin court-persona needs
COPY cmd/court-persona ./cmd/court-persona
COPY internal ./internal

# Fully static binary (no CGO) — required for minimal scratch/alpine rootfs in Firecracker
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /court-persona ./cmd/court-persona

# === Runtime stage (very minimal) ===
FROM alpine:3.18

RUN apk add --no-cache ca-certificates tzdata

# Non-root (defense in depth for untrusted Court Persona VMs)
RUN addgroup -S aegis && adduser -S -G aegis aegis
USER aegis

WORKDIR /app

COPY --from=builder /court-persona /usr/local/bin/court-persona

# Minimal init script for Firecracker boot.
# We explicitly redirect all output to the serial console so that
# our debug prints (and any crashes) appear in the host-captured
# fc-court-persona-console.log.
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

# Court persona identity injected by orchestrator on kernel cmdline (aegis.persona=xxx)
# so a single court-persona.img works for all 7 personas. We forward it both as env
# (for resolvePersona) and as explicit --persona flag (to satisfy cobra MarkFlagRequired
# during direct exec from /init with no argv). See resolvePersona, firecracker.go, and
# governance-court.md.
PERSONA=""
if [ -r /proc/cmdline ]; then
  for kv in $(cat /proc/cmdline 2>/dev/null || true); do
    case "$kv" in
      aegis.persona=*)
        PERSONA="${kv#aegis.persona=}"
        export AEGIS_COURT_PERSONA="$PERSONA"
        ;;
    esac
  done
fi

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
if [ -n "$PERSONA" ]; then
  echo "=== COURT-PERSONA GUEST /init STARTING (persona=$PERSONA) ==="
else
  echo "=== COURT-PERSONA GUEST /init STARTING ==="
fi
ts=$(date +%s 2>/dev/null || echo 0)000000000
echo "BOOT_TIMING: phase=/init_exec_binary ts=${ts} duration_ms=0"

if [ -n "$PERSONA" ]; then
  exec /usr/local/bin/court-persona --persona="$PERSONA"
else
  exec /usr/local/bin/court-persona
fi
EOF

USER root
RUN chmod +x /init
USER aegis

# Use /init so the image can boot as a real Firecracker microVM.
ENTRYPOINT ["/init"]

# The thin main is the entrypoint.
# It expects:
#   - Distributed per-VM private key via AEGIS_VM_PRIVATE_KEY_PATH (or default location in /tmp or state)
#   - Persona identity via --persona flag, AEGIS_COURT_PERSONA env, or kernel cmdline
#   - To register with AegisHub (unix inside dev guests or vsock:9999 for real Firecracker)
#   - To pull proposals directly from Store and submit signed votes to Court Scribe (never via Scribe content path)
#   - Real LLM calls via "llm.call" to network-boundary (no mocks in prod path)

# Notes:
# - This binary contains the complete real Court Persona implementation (no surface stubs or mock LLM).
# - 7 distinct instances are launched by the orchestrator (one per persona) using the same image + identity injection.
# - All votes are Ed25519 signed. Structured output is strictly enforced.
# - When launched inside Firecracker, the orchestrator distributes the ephemeral 0600 key file (see orchestrator.go key hygiene).
# - Future: health/readiness for the critical component watchdog (already lists court-persona as critical).