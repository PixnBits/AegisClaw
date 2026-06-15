# cmd/project-manager/Dockerfile
# Project Manager guest for collaboration model (user.goal → real LLM plan → channel post).

FROM golang:1.24-alpine AS builder

ENV GOTOOLCHAIN=auto
WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/project-manager ./cmd/project-manager
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /project-manager ./cmd/project-manager

FROM alpine:3.18

RUN apk add --no-cache ca-certificates tzdata
RUN addgroup -S aegis && adduser -S -G aegis aegis

WORKDIR /app

COPY --from=builder /project-manager /usr/local/bin/project-manager

COPY <<'EOF' /init
#!/bin/sh
export PATH=/usr/local/bin:/usr/bin:/bin
export HOME=/root

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

mkdir -p /etc/aegis 2>/dev/null || true

if grep -q 'aegis.boot_timing=1' /proc/cmdline 2>/dev/null || [ "${AEGIS_BOOT_TIMING:-0}" = "1" ]; then
  export AEGIS_BOOT_TIMING=1
  ts=$(date +%s 2>/dev/null || echo 0)000000000
  echo "BOOT_TIMING: phase=/init_boot_timing_enabled ts=${ts} duration_ms=0"
fi

ts=$(date +%s 2>/dev/null || echo 0)000000000
echo "BOOT_TIMING: phase=/init_start ts=${ts} duration_ms=0"

exec >/dev/console 2>&1
echo "=== project-manager guest /init: entropy ready, key path=${AEGIS_VM_PRIVATE_KEY_PATH:-/etc/aegis/vmkey} ==="
ts=$(date +%s 2>/dev/null || echo 0)000000000
echo "BOOT_TIMING: phase=/init_exec_binary ts=${ts} duration_ms=0"
/usr/local/bin/project-manager || {
  echo "=== project-manager exited $?, holding guest open for logs ==="
  sleep 86400
}
EOF

RUN chmod +x /init

ENTRYPOINT ["/init"]