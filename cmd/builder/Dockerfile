# cmd/builder/Dockerfile
# Multi-stage build for Builder VM rootfs (Phase 4)
# Provides the builder binary + minimal scanners for the 5 mandatory security gates.
# Aligned with:
#   - docs/specs/builder-security-gates.md (SAST, SCA, Secrets with vague errors, Policy-as-Code, Composition)
#   - docs/specs/builder-vm.md (ephemeral, untrusted, minimal privileges)
#   - docs/specs/threat-model.md & security-model.md (minimal image, supply-chain hardening)

# === Build stage ===
FROM golang:1.24-alpine AS builder

# Allow the container to download a newer Go toolchain if the module requires it
# (go.mod currently pins 1.26.2). This makes the image build resilient when the
# official base image lags behind the repo's declared Go version.
ENV GOTOOLCHAIN=auto

WORKDIR /app

# Minimal build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Go module caching (build context is repo root for the builder component)
COPY go.mod go.sum ./
RUN go mod download

# Copy only builder + any shared packages it needs
COPY cmd/builder ./cmd/builder
# COPY internal ./internal   # uncomment if/when shared internal packages are used by builder

# Statically build the builder binary (no CGO for small attack surface)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /builder ./cmd/builder

# === Install minimal scanners for the 5 gates ===
# SAST (Go-focused): gosec
RUN go install github.com/securego/gosec/v2/cmd/gosec@latest

# SCA (Go native, no external deps): govulncheck
RUN go install golang.org/x/vuln/cmd/govulncheck@latest

# Policy-as-Code: OPA (static binary, small footprint)
RUN wget -q https://github.com/open-policy-agent/opa/releases/download/v0.58.0/opa_linux_amd64 -O /usr/local/bin/opa \
    && chmod +x /usr/local/bin/opa

# Secrets scanning: gitleaks (lightweight Go binary)
RUN wget -q https://github.com/gitleaks/gitleaks/releases/download/v8.18.2/gitleaks_8.18.2_linux_x64.tar.gz -O - \
    | tar -xz -C /usr/local/bin gitleaks \
    && chmod +x /usr/local/bin/gitleaks

# === Runtime stage (minimal) ===
FROM alpine:3.18

# Only essential runtime packages (no compilers, minimal attack surface)
RUN apk add --no-cache ca-certificates tzdata git

# Copy artifacts from build stage
COPY --from=builder /builder /usr/local/bin/builder
COPY --from=builder /go/bin/gosec /usr/local/bin/gosec
COPY --from=builder /go/bin/govulncheck /usr/local/bin/govulncheck
COPY --from=builder /usr/local/bin/opa /usr/local/bin/opa
COPY --from=builder /usr/local/bin/gitleaks /usr/local/bin/gitleaks

# Non-root user (defense-in-depth for the untrusted Builder VM)
RUN addgroup -S aegis && adduser -S -G aegis aegis
USER aegis

WORKDIR /app

# Entrypoint (Firecracker config usually overrides CMD)
ENTRYPOINT ["/usr/local/bin/builder"]

# Notes for rootfs usage:
# - This image is exported by scripts/build-microvms-docker.sh into per-component rootfs tarballs.
# - Scanners are available inside the VM for gate execution (see runSecurityGates in main.go).
# - Keep this image small. Prefer static binaries.
# - Future: Add SBOM generation step (e.g. syft) and image signing.
# - Cross-reference: full-implementation-roadmap.md Phase 4 and 00-v2-phased-implementation-plan.md