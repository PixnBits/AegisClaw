# AegisClaw — Makefile
#
# Targets:
#   build                      — build everything (binaries + rootfs images)
#   build-binaries             — build the aegisclaw and guest-agent binaries only
#   build-rootfs               — build all microVM rootfs images
#   build-rootfs-guest         — build guest-agent rootfs (default sandbox images)
#   build-rootfs-aegishub      — build AegisHub system microVM rootfs
#   build-rootfs-portal        — build dashboard portal microVM rootfs
#   build-rootfs-builder       — build builder rootfs (Go + git + dev tools)
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

BINARY_AEGISCLAW  := aegisclaw
BINARY_GUEST_AGENT := guest-agent
GOFLAGS           :=

.PHONY: build build-binaries build-rootfs build-rootfs-guest build-rootfs-aegishub \
        build-rootfs-portal build-rootfs-builder test test-short test-inprocess \
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

# ── build-rootfs ──────────────────────────────────────────────────────────────
#
# Build microVM rootfs images for Firecracker sandboxes.
#
# Prerequisites:
#   - root privileges (sudo)
#   - e2fsprogs (mkfs.ext4, e2fsck, resize2fs)
#   - For builder rootfs: docker
#
# The images are installed to /var/lib/aegisclaw/rootfs-templates/
#

## build-rootfs: build all microVM rootfs images.
build-rootfs: build-rootfs-guest build-rootfs-aegishub build-rootfs-portal build-rootfs-builder

## build-rootfs-guest: build guest-agent rootfs (default sandbox VMs: agent, court, builder, skills).
build-rootfs-guest:
	sudo ./scripts/build-rootfs.sh --target=guest

## build-rootfs-aegishub: build AegisHub system microVM rootfs (IPC router).
build-rootfs-aegishub:
	sudo ./scripts/build-rootfs.sh --target=aegishub

## build-rootfs-portal: build dashboard portal microVM rootfs.
build-rootfs-portal:
	sudo ./scripts/build-rootfs.sh --target=portal

## build-rootfs-builder: build builder rootfs (Go + git + golangci-lint + staticcheck + make).
build-rootfs-builder:
	sudo ./scripts/build-builder-rootfs.sh /var/lib/aegisclaw/rootfs-templates/builder.ext4

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

## record-cassettes: re-record ALL Ollama cassettes (requires root + KVM + Ollama).
record-cassettes: record-cassette-time record-cassette-solar record-cassette-hello-world record-cassette-tutorial

## record-cassette-time: re-record the time-question chat scenario cassette.
record-cassette-time:
	RECORD_OLLAMA=true \
	  go test ./cmd/aegisclaw \
	    -run TestChatMessageLiveScenarioTimeQuestion \
	    -count=1 -timeout 10m -v

## record-cassette-hello-world: re-record the hello-world skill chat scenario cassette.
record-cassette-hello-world:
	RECORD_OLLAMA=true \
	  go test ./cmd/aegisclaw \
	    -run TestChatMessageLiveScenarioHelloWorldSkill \
	    -count=1 -timeout 30m -v

## record-cassette-solar: re-record the solar sizing chat scenario cassette.
record-cassette-solar:
	RECORD_OLLAMA=true \
	  go test ./cmd/aegisclaw \
	    -run TestChatMessageLiveScenarioSolarSizing \
	    -count=1 -timeout 10m -v

## record-cassette-tutorial: re-record the first-skill-tutorial live cassette.
record-cassette-tutorial:
	RECORD_OLLAMA=true \
	  go test ./cmd/aegisclaw -tags=livetest \
	    -run TestFirstSkillTutorialLive \
	    -count=1 -timeout 60m -v

# ── vet ───────────────────────────────────────────────────────────────────────

vet:
	go vet ./...

# ── clean ─────────────────────────────────────────────────────────────────────

clean:
	rm -f $(BINARY_AEGISCLAW) $(BINARY_GUEST_AGENT)
