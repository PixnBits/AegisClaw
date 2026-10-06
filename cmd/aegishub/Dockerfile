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
FROM golang:1.24-alpine@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191 AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/aegishub ./cmd/aegishub
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /aegishub ./cmd/aegishub

# === Runtime stage ===
# TODO(#127 follow-up): alpine 3.18 is EOL. When replacing it, bump the tag and the @sha256 digest together.
FROM alpine:3.18@sha256:de0eb0b3f2a47ba1eb89389859a9bd88b28e82f5826b6969ad604979713c2d4f

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -S aegis && adduser -S -G aegis aegis
USER aegis

WORKDIR /app

COPY --from=builder /aegishub /usr/local/bin/aegishub

# The hub binary is started with the "start" subcommand by the daemon.
# It listens on the configured unix socket (or vsock in real Firecracker guests).
CMD ["/usr/local/bin/aegishub", "start"]
