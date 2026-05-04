# AegisClaw — Makefile
#
# Targets:
#   build                      — build everything (binaries + rootfs images)
#   build-binaries             — build the aegisclaw and guest-agent binaries only
#   build-rootfs               — build all microVM rootfs images using Docker templates (fast/cacheable)
#   test                       — run all unit and integration tests (no Firecracker required)
#   test-short                 — run only fast unit tests (skip heavy journey tests)
#   test-inprocess             — run in-process integration tests (test-only, no KVM needed)
#   record-cassettes           — re-record ALL Ollama cassettes (requires root + KVM + Ollama)
#   record-cassette-time       — re-record the time-question cassette only
#   record-cassette-hello-world — re-record the hello-world-skill cassette only
#   record-cassette-solar      — re-record the solar-sizing cassette only
#   record-cassette-tutorial   — re-record the first-skill-tutorial cassette only
#   vet                        — run go vet on all packages
#   clean                      — remove build artifacts
#
BINARY_AEGISCLAW  := aegisclaw
BINARY_GUEST_AGENT := guest-agent
GOFLAGS           :=

.PHONY: build build-binaries build-rootfs test test-short test-inprocess \
        record-cassettes \
        record-cassette-time record-cassette-hello-world \
        record-cassette-solar record-cassette-tutorial \
        vet clean

# ── build ─────────────────────────────────────────────────────────────────────
## build: build everything (binaries and all microVM rootfs images).
build: build-binaries build-rootfs

## build-binaries: build the aegisclaw and guest-agent binaries only.
build-binaries:
	go build $(GOFLAGS) -o $(BINARY_AEGISCLAW) ./cmd/aegisclaw
	go build $(GOFLAGS) -o $(BINARY_GUEST_AGENT) ./cmd/guest-agent

# ── build-rootfs ─────────────────────────────────────────────────────────────
#
# Build microVM rootfs images for Firecracker sandboxes using Docker templates.
# Docker is used to produce cacheable, reproducible Alpine base layers; the
# freshly compiled Go binaries are injected afterwards on the host.
#
# Prerequisites:
#   - root privileges (sudo)
#   - docker
#   - e2fsprogs (mkfs.ext4, e2fsck, resize2fs)
#
# The images are installed to /var/lib/aegisclaw/rootfs-templates/
# Use --target=<name> to build a single image (guest, aegishub, portal, builder).
#
## build-rootfs: build all microVM rootfs images.
build-rootfs:
	sudo ./scripts/build-microvms-docker.sh

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
## test-inprocess: run in-call integration tests (no KVM, test-only executor).
test-inprocess:
	AEGISCLAW_INPROCESS_TEST_MODE=unsafe_for_testing_only \
	  go test ./cmd/aegisclaw \
	    -tags=inprocesstest \
	    -run 'Integration|Journey|InProcess' \
	    -count=1 \
	    -v

# ── Ollama cassette recording ─────────────────────────────────────────────────
#
# Cassettes capture live Ollama HTTP exchanges and replay them deterministically
# so tests pass without a live Ollama daemon.  Regenerate cassettes after:
#   • Changing model prompts or system messages
#   • Updating the agent ReAct loop logic
#   • Bumping Ollama or model versions
#   • Any change that makes the existing cassette responses inconsistent
#
# Prerequisites (all four required):
#   1. Run as root   — Firecracker jailer needs CAP_SYS_ADMIN
#   2. /dev/kvm      — hardware virtualisation
#   3. Alpine rootfs — /var/lib/aegisclaw/rootfs-templates/alpine.ext4
#   4. Ollama daemon — listening on 127.0.0.1:11434 with the required models
#
# Models used:
#   • mistral-nemo:latest  (main agent)
#   • llama3.2:3b          (court reviewer)
#
# Pull them first:
#   ollama pull mistral-nemo:latest
#   ollama pull llama3.2:3b
#
# Cassette files are stored in testdata/cassettes/ and are committed to the
# repository so CI can replay them without a live Ollama instance.
#
# Usage examples:
#   make record-cassettes                    # refresh all cassettes
#   make record-cassette-time               # refresh one scenario
#   RECORD_OLLAMA=true go test ./cmd/aegisclaw -run TestChatMessageLiveScenarioTimeQuestion -v
#
## record-cassettes: re-record ALL Ollama cassettes (requires root + KVM + Ollama).
record-cassettes: record-cassette-time record-cassette-solar record-cassette-hello-world record-cassette-tutorial
# ...
