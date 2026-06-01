# cmd/network-boundary/Dockerfile
# Container image for the Network Boundary VM (Envoy + Go control plane).
# The only component allowed to hold secrets or make outbound network calls.
#
# SPEC REFERENCES:
#   - docs/specs/network-boundary.md (Envoy proxy, dynamic routes from Store,
#     secret injection, per-skill allowlists, crash = block all egress)
#   - docs/specs/secret-management.md
#   - threat-model.md (critical containment boundary)

# === Build stage ===
FROM golang:1.24-alpine AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/network-boundary ./cmd/network-boundary
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /network-boundary ./cmd/network-boundary

# === Runtime stage ===
# Note: Real Network Boundary images need a working Envoy binary.
# Alpine 3.18 does not ship "envoy" in its repos, so we install the Go control plane
# only here. In a production setup you would either:
#   - Use a multi-stage from envoyproxy/envoy and copy the binary, or
#   - Download a static envoy build during image creation.
# The control plane will still start; full egress proxying requires Envoy present.
FROM alpine:3.18

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -S aegis && adduser -S -G aegis aegis

WORKDIR /app

COPY --from=builder /network-boundary /usr/local/bin/network-boundary

# Minimal init script for Firecracker.
# This brings up basic environment and then execs the real component.
# Firecracker will run /init as PID 1 when booting the kernel with this rootfs.
COPY <<'EOF' /init
#!/bin/sh
# Basic environment setup for the guest
export PATH=/usr/local/bin:/usr/bin:/bin
export HOME=/root

# Mount basic filesystems if needed (Firecracker often provides these)
mount -t proc proc /proc 2>/dev/null || true
mount -t sysfs sys /sys 2>/dev/null || true
mount -t devtmpfs dev /dev 2>/dev/null || true

if [ -r /proc/sys/kernel/random/write_wakeup_threshold ]; then
  echo 1024 > /proc/sys/kernel/random/write_wakeup_threshold 2>/dev/null || true
fi
i=0
while [ "$i" -lt 60 ]; do
  if dd if=/dev/urandom of=/dev/null bs=32 count=1 2>/dev/null; then
    break
  fi
  i=$((i + 1))
  sleep 0.05
done

exec >/dev/console 2>&1
echo "=== network-boundary guest /init: entropy ready, starting boundary ==="
exec /usr/local/bin/network-boundary
EOF

RUN chmod +x /init

# Use the minimal init as entrypoint for the image (Firecracker will use it as the kernel's init).
ENTRYPOINT ["/init"]
