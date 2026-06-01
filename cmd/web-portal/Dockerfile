# cmd/web-portal/Dockerfile
# Minimal container image for the Web Portal VM (thin presentation layer only).
#
# Exported by scripts/build-microvms-docker.sh into web-portal.img.tar.gz.
#
# SPEC REFERENCES:
#   - docs/specs/web-portal-vm.md (started exclusively by Host Daemon, receives traffic only
#     via the daemon's hardened reverse proxy, no business logic, no secrets)
#   - docs/specs/web-portal.md (thin layer; all actions go through AegisHub)
#   - host-daemon.md (lifecycle managed by daemon; reverse proxy on :8080)

# === Build stage ===
FROM golang:1.24-alpine AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/web-portal ./cmd/web-portal
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /web-portal ./cmd/web-portal

# === Runtime stage ===
FROM alpine:3.18

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -S aegis && adduser -S -G aegis aegis

WORKDIR /app

COPY --from=builder /web-portal /usr/local/bin/web-portal

# Minimal init script for Firecracker boot.
# We explicitly redirect all output to the serial console so that
# our debug prints (and any crashes) appear in the host-captured
# fc-web-portal-console.log.
COPY <<'EOF' /init
#!/bin/sh
export PATH=/usr/local/bin:/usr/bin:/bin
export HOME=/root

mount -t proc proc /proc 2>/dev/null || true
mount -t sysfs sys /sys 2>/dev/null || true
mount -t devtmpfs dev /dev 2>/dev/null || true

# Seed entropy early so crypto/rand does not block guest startup for ~60s (502 on host proxy).
if [ -r /proc/sys/kernel/random/write_wakeup_threshold ]; then
  echo 1024 > /proc/sys/kernel/random/write_wakeup_threshold 2>/dev/null || true
fi
dd if=/dev/urandom of=/dev/null bs=32 count=1 2>/dev/null || true

# Force all stdout + stderr to the serial console.
# This is the most reliable way to get application output visible
# in Firecracker's captured console log for minimal guests.
exec >/dev/console 2>&1

echo "=== WEB-PORTAL GUEST /init STARTING ==="

exec /usr/local/bin/web-portal
EOF

RUN chmod +x /init

# Use /init so the image can boot as a real Firecracker microVM.
ENTRYPOINT ["/init"]
