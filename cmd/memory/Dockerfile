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
USER aegis

WORKDIR /app

COPY --from=builder /memory /usr/local/bin/memory

# The thin memory main (cmd/memory/main.go) is the entrypoint.
# It loads its distributed private key, registers with Hub, and serves the real
# Memory VM (internal/memory) over the hubclient (unix or vsock).
ENTRYPOINT ["/usr/local/bin/memory"]

# Notes:
# - Implements hard-bounded short-term context + TTL long-term + explicit zeroization.
# - Enforces per-agent ACLs (one agent can never read another agent's memories).
# - Paired launch in orchestrator guarantees the 1:1 relationship required by the spec.