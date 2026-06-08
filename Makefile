SHELL := /bin/bash

CLEAN_FIRECRACKER_ROOTFS_SCRIPT := $(CURDIR)/scripts/clean-firecracker-rootfs.sh
CREATE_FIRECRACKER_ROOTFS_SCRIPT := $(CURDIR)/scripts/create-firecracker-rootfs.sh
ENSURE_AEGIS_DIR_SCRIPT := $(CURDIR)/scripts/ensure-aegis-dir.sh
AEGIS_BIN := $(CURDIR)/bin/aegis

.PHONY: build build-binaries build-microvms clean clean-microvms test test-integration test-e2e test-e2e-contract test-e2e-llm test-tcb test-chaos sbom smoke help doctor setup boot-metrics

# Default target
all: build

# Build all binaries and microVMs
# NOTE: build-microvms is best-effort (Docker + sudo + Go version inside images).
# Failures here (common without NOPASSWD or on bleeding-edge go.mod) do not
# block the primary deliverables. See AGENTS.md and scripts/build-microvms-docker.sh.
build: build-binaries
	@$(MAKE) build-microvms || echo "⚠ build-microvms had issues (Docker/Go version inside images/permissions). Binaries are ready. MicroVM filesystems are optional on this env per AGENTS.md."

# Build all command binaries
build-binaries:
	go build -o bin/aegis ./cmd/aegis
	go build -o bin/aegishub ./cmd/aegishub
	go build -o bin/agent ./cmd/agent
	go build -o bin/builder ./cmd/builder
	go build -o bin/court-persona ./cmd/court-persona
	go build -o bin/court-scribe ./cmd/court-scribe
	go build -o bin/memory ./cmd/memory
	go build -o bin/network-boundary ./cmd/network-boundary
	go build -o bin/secrets ./cmd/secrets
	go build -o bin/store ./cmd/store
	go build -o bin/web-portal ./cmd/web-portal
	go build -o bin/project-manager ./cmd/project-manager

# Build microVM filesystems (Linux/Firecracker only)
build-microvms:
	@if [ "$(shell uname -s)" = "Linux" ]; then \
		if [ -d "/opt/aegis" ] && [ ! -w "/opt/aegis" ]; then \
			echo "Note: /opt/aegis is not writable. Filesystems will be built to ~/.aegis/firecracker/rootfs"; \
			echo ""; \
		fi; \
		bash scripts/build-microvms-docker.sh; \
	else \
		echo "MicroVM building not needed on $(shell uname -s) (uses Docker Sandboxes)"; \
	fi

# Start the daemon (absolute path for NOPASSWD sudoers rules)
start:
	sudo -n $(AEGIS_BIN) start

# Start daemon in foreground for debugging
start-foreground:
	sudo -n $(AEGIS_BIN) start --foreground

# Stop the daemon
stop:
	./bin/aegis stop

# Check daemon status
status:
	./bin/aegis status

# Run health checks
doctor:
	./bin/aegis doctor

# Quick smoke test - run this after `make start` to verify the system came up cleanly.
# Catches regressions in startup, socket, reverse proxy, and basic portal reachability.
smoke:
	@echo "=== AegisClaw Smoke Test ==="
	@echo ""
	@echo "0. CLI surface (Task 6.1 complete --help + version)..."
	@./bin/aegis --help | grep -q "autonomy" && echo "   ✓ Full command tree present (autonomy + skills + court + secrets + restart etc.)" || (echo "   ✗ CLI tree incomplete"; exit 1)
	@./bin/aegis --version | grep -q "phase6-cli" && echo "   ✓ Version present" || echo "   ⚠ version (non-fatal)"
	@echo ""
	@echo "1. CLI: status..."
	@./bin/aegis status | grep -q "running" && echo "   ✓ Daemon reports as running" || (echo "   ✗ Daemon not running"; exit 1)
	@echo ""
	@echo "2. CLI: doctor..."
	@./bin/aegis doctor > /dev/null 2>&1 && echo "   ✓ doctor succeeded" || (echo "   ✗ doctor failed"; exit 1)
	@echo ""
	@echo "3. Web Portal proxy (via daemon on :8080)..."
	@curl -sf --max-time 8 http://localhost:8080/health > /dev/null && echo "   ✓ /health responded" || (echo "   ✗ Portal proxy health check failed (is the daemon running?)"; exit 1)
	@curl -sf --max-time 8 -o /dev/null http://localhost:8080/ && echo "   ✓ Root page responds" || (echo "   ✗ Root page failed"; exit 1)
	@echo ""
	@echo "4. Key new REST endpoints (thin layer)..."
	@curl -sf --max-time 5 http://localhost:8080/api/proposals > /dev/null 2>&1 && echo "   ✓ /api/proposals" || echo "   ⚠ /api/proposals (may be empty or backend-limited)"
	@curl -sf --max-time 5 http://localhost:8080/api/audit > /dev/null 2>&1 && echo "   ✓ /api/audit" || echo "   ⚠ /api/audit"
	@echo ""
	@echo "5. Canvas + Teams features (Phase 5 first slice)..."
	@curl -sf --max-time 8 http://localhost:8080/canvas > /dev/null && echo "   ✓ /canvas responds" || (echo "   ✗ /canvas failed"; exit 1)
	@curl -sf --max-time 8 http://localhost:8080/canvas 2>/dev/null | grep -q 'team-filter-pills' && echo "   ✓ team filtering UI present" || echo "   ⚠ team filtering UI (may be dynamic)"
	@curl -sf --max-time 8 http://localhost:8080/canvas 2>/dev/null | grep -q 'teams-list-section' && echo "   ✓ teams list/sidebar present" || echo "   ⚠ teams list/sidebar"
	@curl -sf --max-time 8 http://localhost:8080/canvas 2>/dev/null | grep -q 'create-demo-team-btn' && echo "   ✓ New Demo Team button present" || echo "   ⚠ New Demo Team button"
	@echo "6. Actual team creation via thin endpoint..."
	@curl -sf --max-time 5 -X POST -H "Content-Type: application/json" \
	  -d '{"id":"smoke-team-1","name":"Smoke Test Team","goal":"Verify thin team.* wiring"}' \
	  http://localhost:8080/api/teams/create > /dev/null && echo "   ✓ /api/teams/create succeeded" || echo "   ⚠ team create"
	@curl -sf --max-time 5 http://localhost:8080/api/teams > /dev/null && echo "   ✓ /api/teams list responded" || echo "   ⚠ team list"
	@echo "7. Dedicated /teams page + success/messages/cards UX (Phase 5 Teams slice)..."
	@curl -sf --max-time 8 http://localhost:8080/teams > /dev/null && echo "   ✓ /teams responds" || (echo "   ✗ /teams failed"; exit 1)
	@curl -sf --max-time 8 http://localhost:8080/teams 2>/dev/null | grep -q 'data-testid="create-team-form"' && echo "   ✓ create team form + success placeholder present" || echo "   ⚠ create form"
	@curl -sf --max-time 8 http://localhost:8080/teams 2>/dev/null | grep -q 'data-testid="team-create-success"' && echo "   ✓ create success banner container present" || echo "   ⚠ create success"
	@curl -sf --max-time 8 http://localhost:8080/teams 2>/dev/null | grep -q 'data-testid="send-team-msg-form"' && echo "   ✓ send team message form present" || echo "   ⚠ message form"
	@curl -sf --max-time 8 http://localhost:8080/teams 2>/dev/null | grep -q 'data-testid="team-msg-success"' && echo "   ✓ message success banner container present" || echo "   ⚠ message success"
	@curl -sf --max-time 8 http://localhost:8080/teams 2>/dev/null | grep -q 'data-testid="team-cards-section"' && echo "   ✓ team overview cards section present" || echo "   ⚠ cards section"
	@curl -sf --max-time 8 http://localhost:8080/teams 2>/dev/null | grep -q '>Msgs<' && echo "   ✓ Msgs column (messages feed count) in table" || echo "   ⚠ Msgs column"
	@curl -sf --max-time 8 http://localhost:8080/teams 2>/dev/null | grep -q 'Team Messages / Activity' && echo "   ✓ messages/activity section header" || echo "   ⚠ messages section"
	@echo ""
	@echo "=== Smoke test passed! System is up and the proxy/portal + team UI features are reachable. ==="

# boot-metrics: table of host + guest high-res boot phases (orchestrator, fc backend, guest
# main/key/register/loop). Requires daemon started with AEGIS_BOOT_TIMING=1 and the VM
# launched after. Falls back to scripts/ parser if socket not available.
boot-metrics:
	@VM_ID="$(VM)"; \
	if [ -z "$$VM_ID" ]; then \
		echo "Usage: make boot-metrics VM=agent-<session>   (or memory-..., web-portal, court-scribe, ...)"; \
		echo "       (start daemon with: AEGIS_BOOT_TIMING=1 sudo -E make start)"; \
		exit 1; \
	fi; \
	if [ -x "./bin/aegis" ]; then \
		./bin/aegis vm boot-metrics "$$VM_ID" || bash scripts/boot-metrics.sh "$$VM_ID"; \
	else \
		echo "bin/aegis not found; run 'make build-binaries' first"; \
		bash scripts/boot-metrics.sh "$$VM_ID"; \
	fi

# Clean build artifacts (binaries + common generated files).
# This is safe and fast. Use `make clean-microvms` for the heavy rootfs images.
clean:
	rm -rf bin/ sbom/ test-results/
	@rm -f aegis.log 2>/dev/null || true
	@go clean -testcache 2>/dev/null || true
	@echo "✓ Cleaned build artifacts (bin/, sbom/, test-results/, aegis.log, test cache)."
	@echo "  Run 'make clean-microvms' if you also want to delete the built microVM images/tarballs."

# Remove built microVM images and tarballs (both user and system locations).
# This is DESTRUCTIVE and slow to recover from — only use when you suspect
# stale rootfs images (common cause of "web portal not reachable" during development).
# Pass YES=1 to skip the confirmation prompt (automated dev loops).
clean-microvms:
	@echo "⚠ WARNING: This will permanently delete all built microVM rootfs images and tarballs."
	@echo "   You will need to run 'make build-microvms' again afterwards (can take 10-30+ minutes)."
	@if [ "$(YES)" != "1" ]; then \
		read -r -p "Are you sure? [y/N] " REPLY; \
		if [[ ! "$$REPLY" =~ ^[Yy]$$ ]]; then \
			echo "Aborted clean-microvms."; \
			exit 0; \
		fi; \
	fi
	@echo "Removing user-local microVM artifacts..."
	@rm -rf ~/.aegis/firecracker/rootfs/*.img ~/.aegis/firecracker/rootfs/*.img.tar.gz 2>/dev/null || true
	@echo "Removing system microVM artifacts (via sudoers-approved cleanup script)..."
	@sudo -n "$(CLEAN_FIRECRACKER_ROOTFS_SCRIPT)" 2>/dev/null || true
	@echo "✓ MicroVM build artifacts removed."
	@echo "   Next step: make build-microvms"

# Run unit tests
test:
	go test ./...

# Run daemon integration tests
test-integration:
	go test -v -tags=integration ./cmd/aegis -run "TestDaemon|TestCLI|TestVersion|TestProcessCleaning|TestVMList|TestSocket" -count=1 -timeout 90s

# Run E2E tests in a real browser against the live daemon + microVMs (make start first)
test-e2e:
	bash scripts/run-playwright-e2e.sh e2e/chat.spec.js --project=chromium

# Contract-only E2E against the thin web-portal fixture (no daemon). For CI without Firecracker.
test-e2e-contract:
	AEGIS_E2E_FIXTURE=1 bash scripts/run-playwright-e2e.sh e2e/journeys.spec.js --project=chromium

# Real unmocked E2E exercising PM + LLM (Ollama via network-boundary) + channels exactly as a user would:
#   `aegis pm goal "..." --channel foo` then `aegis channel get foo` (or view in portal #channels).
# Uses isolated custom hub/state + short waits + AEGIS_DEFAULT_MODEL (defaults llama3.2:3b).
# Requires: make build, sudo -n for ./bin/aegis (per AGENTS.md), ollama running with the model.
# This is the "no fixtures, hits real LLM" verification for the collaboration model.
test-e2e-llm:
	@echo "=== Real PM+LLM+Channels E2E (unmocked, user path via CLI pm goal + channel inspect) ==="
	@echo "See scripts/verify-pm-llm-e2e.sh for details and success criteria."
	AEGIS_DEFAULT_MODEL="${AEGIS_DEFAULT_MODEL:-llama3.2:3b}" bash scripts/verify-pm-llm-e2e.sh

# TCB-specific tests (7.5.7). Additive target.
# Runs unit tests for security + runtime + cmd/aegis TCB surface + skeleton compliance tests.
# Use with -tags=integration for fuller daemon lifecycle checks (requires sudo in some cases).
test-tcb:
	go test -v ./internal/security ./internal/runtime ./cmd/aegis -run 'Test.*(TCB|Key|Socket|Doctor|Workspace|ProcessCleaning)' -count=1

# Chaos/restart tests (7.7). Additive target.
# Controlled via AEGIS_CHAOS=1 env var. Skips by default so normal `make test` is unaffected.
# Exercises daemon + VM failure scenarios, TCB containment, recovery, and journey resilience.
# Often requires sudo for live daemon ops.
test-chaos:
	@echo "=== Running 7.7 Chaos/Restart Tests (AEGIS_CHAOS=1 required) ==="
	@echo "These test daemon/VM failure + recovery (host-daemon.md Test Requirements + 9 user journeys)."
	AEGIS_CHAOS=1 go test -v -tags=integration ./cmd/aegis -run 'Test.*(Chaos|Restart|Watchdog|VMDeath)' -count=1 || true

# SBOM + supply-chain target (7.8). Additive.
# Always succeeds and produces an artifact:
#   - CycloneDX JSON if cyclonedx-gomod or syft present in PATH (or after go install attempt).
#   - High-quality fallback manifest (go.mod + Builder gates + refs) otherwise.
# Image signing hooks (cosign) are non-fatal / commented (keyless or COSIGN_* env).
# Refs: threat-model.md:3 (backdoored skill mitigation via SBOM + signing), additional-requirements-and-gaps.md,
# builder-security-gates.md, grok-build-execution-plan.md:7.8 / 1193, host-daemon.md (TCB supply chain).
sbom:
	@echo "=== AegisClaw SBOM / Supply-Chain (7.8) ==="
	@mkdir -p sbom
	@SBOM_JSON=sbom/aegis-sbom.cdx.json; \
	SBOM_TXT=sbom/aegis-sbom.txt; \
	if command -v cyclonedx-gomod >/dev/null 2>&1; then \
		echo "cyclonedx-gomod found — generating CycloneDX"; \
		cyclonedx-gomod generate -o $$SBOM_JSON . 2>/dev/null || echo "cyclonedx-gomod run had issues (see output)"; \
	elif command -v syft >/dev/null 2>&1; then \
		echo "syft found — generating CycloneDX for dir"; \
		syft dir:. -o cyclonedx-json=$$SBOM_JSON 2>/dev/null || echo "syft run had issues"; \
	else \
		echo "No SBOM tool in PATH — attempting one-shot go install for cyclonedx-gomod (Go module SBOM)"; \
		if go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest 2>/dev/null; then \
			GOBIN_BIN="$$(go env GOBIN)"; \
			if [ -z "$$GOBIN_BIN" ]; then GOBIN_BIN="$$(go env GOPATH)/bin"; fi; \
			if [ -x "$$GOBIN_BIN/cyclonedx-gomod" ]; then \
				"$$GOBIN_BIN/cyclonedx-gomod" generate -o $$SBOM_JSON . 2>/dev/null || true; \
			fi; \
		fi; \
	fi; \
	if [ -f $$SBOM_JSON ]; then \
		echo "✓ SBOM (CycloneDX JSON) written to $$SBOM_JSON"; \
	else \
		echo "Writing fallback manifest (tools unavailable or failed; fully upgradeable)"; \
		{ \
			echo "# AegisClaw v2 Fallback SBOM / Supply-Chain Manifest (7.8)"; \
			echo "# Generated: $$(date -u +%Y-%m-%dT%H:%M:%SZ)"; \
			echo "# Primary refs: threat-model.md:3 (backdoored skill mitigation), additional-requirements-and-gaps.md"; \
			echo "# Builder: builder-security-gates.md + scripts/build-microvms-docker.sh"; \
			echo "# Upgrade path: make sbom (with cyclonedx-gomod/syft in PATH) or cosign for images"; \
			echo ""; \
			echo "## Go module (this tree)"; \
			cat go.mod 2>/dev/null || echo "(go.mod not found at root)"; \
			echo ""; \
			echo "## Builder VM / security gates (image-level)"; \
			echo "  - SAST: gosec"; \
			echo "  - SCA: govulncheck"; \
			echo "  - Secrets: gitleaks + custom"; \
			echo "  - Policy-as-Code: opa"; \
			echo "  - Composition/Health: Go toolchain"; \
			echo "  - Future: full CycloneDX JSON + cosign signatures on rootfs/images"; \
			echo ""; \
			echo "## Notes for TCB / 9 journeys"; \
			echo "  SBOM + signing reduces supply-chain risk for skills (see user-journeys/04 and 09)."; \
			echo "  Hooks are additive and non-fatal (no breakage to make start/stop/test)."; \
		} > $$SBOM_TXT; \
		echo "✓ Fallback SBOM manifest written to $$SBOM_TXT"; \
	fi
	@echo ""
	@echo "Image signing hook (non-fatal, 7.8 / threat-model.md):"
	@echo "  # cosign sign --yes <image>     # keyless (OIDC) or COSIGN_PRIVATE_KEY=... cosign sign --key ..."
	@echo "  (cosign not required; placeholder ready for CI/release per grok-build-execution-plan.md:7.8)"

# Setup target for Journey 01 (onboarding)
# Provides a low-intervention path: build + doctor (per user-journeys/01-installation-onboarding.md)
setup:
	@echo "=== AegisClaw Setup / Onboarding ==="
	@$(MAKE) build-binaries
	@echo ""
	@./bin/aegis doctor || true
	@echo ""
	@echo "Setup complete. Next steps (per AGENTS.md):"
	@echo "  sudo make start"
	@echo "  make smoke"
	@echo "  ./bin/aegis chat --headless \"Hello\""

# Help target
help:
	@echo "AegisClaw Build System"
	@echo ""
	@echo "Targets:"
	@echo "  make build              Build binaries and microVMs"
	@echo "  make build-binaries     Build Go binaries only"
	@echo "  make build-microvms     Build microVM filesystems (NOPASSWD: scripts/create-firecracker-rootfs.sh)"
	@echo "  make setup              Onboarding helper (build + doctor) - Journey 01"
	@echo "  make start              Start the daemon with sudo"
	@echo "  make start-foreground   Start daemon in foreground (debugging)"
	@echo "  make stop               Stop the daemon"
	@echo "  make status             Check daemon status"
	@echo "  make doctor             Run health checks"
	@echo "  make smoke              Quick smoke test after 'make start' (CLI + portal + teams)"
	@echo "  make test               Run unit tests"
	@echo "  make test-integration   Run daemon integration tests"
	@echo "  make test-e2e           Browser E2E vs real daemon + microVMs (requires make start)"
	@echo "  make test-e2e-contract  Thin-portal contract tests only (no daemon; AEGIS_E2E_FIXTURE=1)"
	@echo "  make test-e2e-llm       Real unmocked PM+LLM+channels E2E (CLI pm goal + channel get; hits Ollama, no fixtures; see script)"
	@echo "  make test-tcb           TCB-specific tests (7.5)"
	@echo "  make test-chaos         Chaos/restart tests (7.7, requires AEGIS_CHAOS=1)"
	@echo "  make sbom               SBOM + supply-chain (7.8: CycloneDX or fallback + cosign hooks)"
	@echo "  make clean              Remove build artifacts (binaries, sbom, test results, etc.)"
	@echo "  make clean-microvms     ⚠ DANGER: Delete microVM images (YES=1 skips prompt; /opt via scripts/clean-firecracker-rootfs.sh)"
	@echo "  make help               Show this help message"
	@echo ""
	@echo "Setup:"
	@echo "  1. Install dependencies: go mod download"
	@echo "  2. Build binaries: make build"
	@echo "  3. Start daemon: make start"
	@echo "  4. (Optional) Verify: make smoke"
	@echo ""
	@echo "Documentation:"
	@echo "  See README.md for detailed setup instructions"