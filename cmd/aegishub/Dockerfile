# cmd/aegishub/Dockerfile
# Minimal container image for AegisHub (the central message router).
#
# This is the first component the Host Daemon starts in the base bootstrap sequence.
# All other sandboxes communicate exclusively through it.
#
# SPEC REFERENCES:
#   - docs/specs/aegishub.md (ACL enforcement, signature validation, handshake, deny-by-default)
#   - docs/specs/host-daemon.md (daemon is responsible for starting + key distribution + watchdog)
#   - architecture.md + prd/runtime-architecture.md (the only privileged router)

# === Build stage ===
FROM golang:1.26-alpine3.24@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/aegishub ./cmd/aegishub
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /aegishub ./cmd/aegishub

# === Runtime stage ===
FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -S aegis && adduser -S -G aegis aegis
USER aegis

WORKDIR /app

COPY --from=builder /aegishub /usr/local/bin/aegishub

# The hub binary is started with the "start" subcommand by the daemon.
# It listens on the configured unix socket (or vsock in real Firecracker guests).
CMD ["/usr/local/bin/aegishub", "start"]
