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

# Non-root (defense in depth for the untrusted Agent Runtime VM)
RUN addgroup -S aegis && adduser -S -G aegis aegis
USER aegis

WORKDIR /app

COPY --from=builder /agent /usr/local/bin/agent

# The thin main (cmd/agent/main.go) is the entrypoint.
# It expects:
#   - Distributed per-VM private key via AEGIS_VM_PRIVATE_KEY_PATH (or default location)
#   - To register with AegisHub (unix inside dev guests or vsock:9999 for real Firecracker)
#   - To drive the real 6-step loop via hubclient for LLM + memory + tools
ENTRYPOINT ["/usr/local/bin/agent"]

# Notes:
# - This binary contains the complete real Agent Runtime (no surface stubs).
# - When launched by the orchestrator's StartPairedAgentAndMemory, it receives its own
#   1:1 Memory VM peer and talks exclusively through AegisHub.
# - Future: add health/readiness endpoint if needed for the guest watchdog.