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

exec /usr/local/bin/store
EOF

RUN chmod +x /init

# Use /init so Firecracker can boot this as a real microVM guest.
ENTRYPOINT ["/init"]
