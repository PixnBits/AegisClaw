# cmd/builder/Dockerfile
# Multi-stage build for Builder VM rootfs (Phase 4)
# Provides the builder binary + minimal scanners for the 5 mandatory security gates.
# Aligned with:
#   - docs/specs/builder-security-gates.md (SAST, SCA, Secrets with vague errors, Policy-as-Code, Composition)
#   - docs/specs/builder-vm.md (ephemeral, untrusted, minimal privileges)
#   - docs/specs/threat-model.md & security-model.md (minimal image, supply-chain hardening)

# === Build stage ===
FROM golang:1.26-alpine3.24@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c AS builder

# Pinned tool versions. go install checks module zips against sum.golang.org.
# OPA and gitleaks URLs are linux/amd64 release assets. Checksums are the
# published SHA-256 of those assets.
ARG GOSEC_VERSION=v2.29.0
ARG GOVULNCHECK_VERSION=v1.8.0
ARG OPA_VERSION=v0.58.0
ARG OPA_SHA256=217228eeae07a9790b1d1a3b887cbc3dec253ad6444a7a8eaddbe65fd11968c3
ARG GITLEAKS_VERSION=8.18.2
ARG GITLEAKS_SHA256=6298c9235dfc9278c14b28afd9b7fa4e6f4a289cb1974bd27949fc1e9122bdee

# GOTOOLCHAIN=auto fetches a newer Go patch when go.mod requires one
# (go.mod says go 1.26.2). The build stage is the 1.26 Alpine image.
ENV GOTOOLCHAIN=auto

WORKDIR /app

# Minimal build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Go module caching (build context is repo root for the builder component)
COPY go.mod go.sum ./
RUN go mod download

# Copy only builder + shared packages it imports
# (AegisClaw/internal/workspace and AegisClaw/internal/timing).
COPY cmd/builder ./cmd/builder
COPY internal ./internal

# Statically build the builder binary (no CGO for small attack surface)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /builder ./cmd/builder

# === Install minimal scanners for the 5 gates ===
# SAST (Go-focused): gosec
RUN go install "github.com/securego/gosec/v2/cmd/gosec@${GOSEC_VERSION}"

# SCA (Go native, no external deps): govulncheck
RUN go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"

# Policy-as-Code: OPA (static binary, small footprint). linux/amd64 only.
# Two spaces before the filename: sha256sum text-mode check format.
RUN wget -q "https://github.com/open-policy-agent/opa/releases/download/${OPA_VERSION}/opa_linux_amd64" -O /tmp/opa \
    && echo "${OPA_SHA256}  /tmp/opa" | sha256sum -c - \
    && mv /tmp/opa /usr/local/bin/opa \
    && chmod +x /usr/local/bin/opa

# Secrets scanning: gitleaks (lightweight Go binary). linux/amd64 only.
RUN wget -q "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_x64.tar.gz" -O /tmp/gitleaks.tar.gz \
    && echo "${GITLEAKS_SHA256}  /tmp/gitleaks.tar.gz" | sha256sum -c - \
    && tar -xz -C /usr/local/bin -f /tmp/gitleaks.tar.gz gitleaks \
    && chmod +x /usr/local/bin/gitleaks \
    && rm -f /tmp/gitleaks.tar.gz

# === Runtime stage (minimal) ===
FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

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