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

# The thin main is the entrypoint.
# It expects:
#   - Distributed per-VM private key via AEGIS_VM_PRIVATE_KEY_PATH
#   - To register with AegisHub (unix or vsock:9999)
#   - Never to receive proposal content (strict guard)
#   - To emit full Merkle-signed decision records on review completion
ENTRYPOINT ["/usr/local/bin/court-scribe"]

# Notes:
# - This binary contains the complete real Court Scribe (no surface stubs).
# - Single instance per system. Works with the 7 court-persona-* instances.
# - When launched inside Firecracker, key hygiene and vsock path are identical to other guests.
# - Critical component (watchdog + orchestrator already list it).