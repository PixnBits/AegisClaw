# AegisClaw — Makefile
#
# Targets:
#   build           — build the aegisclaw and guest-agent binaries
#   test            — run all unit and integration tests (no Firecracker required)
#   test-short      — run only fast unit tests (skip heavy journey tests)
#   test-inprocess  — run in-process integration tests (test-only, no KVM needed)
#   vet             — run go vet on all packages
#   clean           — remove build artifacts

BINARY_AEGISCLAW  := aegisclaw
BINARY_GUEST_AGENT := guest-agent
GOFLAGS           :=

.PHONY: build test test-short test-inprocess vet clean

# ── build ─────────────────────────────────────────────────────────────────────

build:
	go build $(GOFLAGS) -o $(BINARY_AEGISCLAW) ./cmd/aegisclaw
	go build $(GOFLAGS) -o $(BINARY_GUEST_AGENT) ./cmd/guest-agent

# ── test ──────────────────────────────────────────────────────────────────────

## test: run all normal tests (unit + journey/integration, no Firecracker).
test:
	go test ./... -count=1

## test-short: run only fast unit tests; skip -short-flagged journey tests.
test-short:
	go test ./... -short -count=1

# ── test-inprocess ────────────────────────────────────────────────────────────
#
# SECURITY WARNING:
#   This target runs the in-process executor which has ZERO sandbox isolation.
#   It exists ONLY for fast developer iteration and CI smoke tests.
#   It MUST NOT be used in production pipelines or release workflows.
#
# Prerequisites:
#   - No KVM / Firecracker required.
#   - The "inprocesstest" build tag enables in-process executor code that is
#     excluded from normal builds.
#   - AEGISCLAW_INPROCESS_TEST_MODE must equal "unsafe_for_testing_only";
#     the executor will panic without it.
#
# Usage:
#   make test-inprocess
#
## test-inprocess: run in-process integration tests (no KVM, test-only executor).
test-inprocess:
	AEGISCLAW_INPROCESS_TEST_MODE=unsafe_for_testing_only \
	  go test ./cmd/aegisclaw \
	    -tags=inprocesstest \
	    -run 'Integration|Journey|InProcess' \
	    -count=1 \
	    -v

# ── vet ───────────────────────────────────────────────────────────────────────

vet:
	go vet ./...

# ── clean ─────────────────────────────────────────────────────────────────────

clean:
	rm -f $(BINARY_AEGISCLAW) $(BINARY_GUEST_AGENT)
