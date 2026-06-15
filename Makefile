SHELL := /bin/bash

CLEAN_FIRECRACKER_ROOTFS_SCRIPT := $(CURDIR)/scripts/clean-firecracker-rootfs.sh
CREATE_FIRECRACKER_ROOTFS_SCRIPT := $(CURDIR)/scripts/create-firecracker-rootfs.sh
ENSURE_AEGIS_DIR_SCRIPT := $(CURDIR)/scripts/ensure-aegis-dir.sh
AEGIS_BIN := $(CURDIR)/bin/aegis

.PHONY: build build-binaries build-microvms clean clean-microvms test test-integration test-e2e test-e2e-contract test-e2e-llm test-e2e-llm-isolated test-tcb test-chaos sbom smoke help doctor setup boot-metrics

# Default target
all: build

# Build host binaries + microVM guest filesystem images (the complete picture).
#
# After editing any source that runs inside a guest (web-portal + internal/dashboard,
# agent + internal/agent, store, memory, court-*, builder, network-boundary, etc.)
# you MUST have up-to-date microVM images, otherwise `./bin/aegis start` will run
# stale binaries inside the Firecracker VMs (e.g. the dashboard that answers on :8080).
#
# This target tries hard to produce a complete, runnable tree:
#   - Always builds the host `bin/*` binaries.
#   - On Linux, also builds the guest rootfs images via build-microvms.
#
# build-microvms requires Docker + (usually) sudo for privileged rootfs steps.
# It is intentionally non-fatal so that `make build` remains useful on macOS,
# in CI without kvm, or before you have set up the sudoers rules.
#
# See:
#   - AGENTS.md ("MicroVM / rootfs builds", "Agent Behavior for Sudo...")
#   - scripts/build-microvms-docker.sh
#   - scripts/aegisclaw-sudoers.example
build: build-binaries
	@echo "==> Host binaries built."
	@if [ "$(shell uname -s)" = "Linux" ]; then \
		echo "==> Building microVM filesystem images (agents, web-portal, store, memory, court, ...)."; \
		echo "    This can take a few minutes on first build or after 'make clean-microvms'."; \
		echo "    It may require Docker and sudo (see AGENTS.md)."; \
		if $(MAKE) --no-print-directory build-microvms; then \
			echo "==> MicroVM images are up to date."; \
		else \
			rc=$$?; \
			echo ""; \
			echo "⚠ WARNING: 'make build-microvms' did not complete successfully (exit $$rc)."; \
			echo ""; \
			echo "   The following components will be STALE inside the guest VMs after"; \
			echo "   './bin/aegis start' (the code that actually runs on :8080, in agents, etc.):"; \
			echo "     - cmd/web-portal/ + internal/dashboard/   (the UI you curl on localhost:8080)"; \
			echo "     - cmd/agent/ + internal/agent/            (the real agent runtime)"; \
			echo "     - cmd/store/, cmd/memory/, court-*, builder, network-boundary, ..."; \
			echo ""; \
			echo "   To get a fully up-to-date system (recommended after source changes):"; \
			echo "     make build-microvms"; \
			echo ""; \
			echo "   Common reasons this happens and how to fix them:"; \
			echo "     - Docker not installed or not running"; \
			echo "     - No permission to /opt/aegis or to create loop devices"; \
			echo "     - Missing NOPASSWD sudo rules for the build scripts"; \
			echo "       (copy scripts/aegisclaw-sudoers.example to /etc/sudoers.d/ and edit)"; \
			echo "     - Running on a system without /dev/kvm (use contract tests instead)"; \
			echo ""; \
			echo "   You can still use the freshly built host binaries for:"; \
			echo "     - 'make test', 'make test-e2e-contract', direct 'bin/web-portal', etc."; \
			echo ""; \
			echo "   Full 'make build' + 'sudo ./bin/aegis start' will only be correct once microvms succeed."; \
			echo ""; \
		fi; \
	else \
		echo "==> Skipping microVM image build (not on Linux; Docker sandboxes are used)."; \
	fi

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

# Quick smoke test - run this after `sudo ./bin/aegis start` to verify the system came up cleanly.
# Per docs/testing-standards.md, this now explicitly asserts the core startup health
# invariants (base infra registration, Court count==7, pre-warm pools, no stray temp
# components, clean status) before deeper portal/teams checks. Failures are loud
# and point to logs/status for diagnosis. This would have caught recent base
# registration / component issues.
#
# IMPORTANT: `sudo ./bin/aegis start` returns quickly once the host daemon detaches.
# On real Firecracker hardware the base (Store + Web Portal + Network Boundary)
# + Court (7 personas) + pre-warm pools boot in the background and can take
# 30s–a few minutes. Poll `make smoke` (or just run `make test-e2e-llm`, which
# has its own readiness wait) until it goes green.
# Run: make smoke (after sudo ./bin/aegis start). LLM agents: run this early on startup changes.
smoke:
	@echo "=== AegisClaw Smoke Test ==="
	@echo "(Asserts startup health invariants from docs/testing-standards.md first.)"
	@echo ""
	@echo "0. CLI surface (Task 6.1 complete --help + version)..."
	@./bin/aegis --help | grep -q "autonomy" && echo "   ✓ Full command tree present (autonomy + skills + court + secrets + restart etc.)" || (echo "   ✗ CLI tree incomplete"; exit 1)
	@./bin/aegis --version | grep -q "phase6-cli" && echo "   ✓ Version present" || echo "   ⚠ version (non-fatal)"
	@echo ""
	@echo "1. CLI: status + startup health invariants (per testing-standards.md)..."
	@STATUS=$$(./bin/aegis status 2>/dev/null); echo "$$STATUS" | grep -Fx 'daemon is running' && echo "   ✓ Daemon reports as running" || (echo "   ✗ Daemon not running"; echo "$$STATUS"; exit 1); \
	  COURT_N=$$(echo "$$STATUS" | sed -n 's/.*Court personas online: \([0-9]*\).*/\1/p' | head -1 || echo 0); \
	  if [ "$$COURT_N" = "7" ]; then echo "   ✓ Court personas online: 7"; else echo "   ✗ Court personas online: $$COURT_N (expected 7 per standards)"; echo "$$STATUS"; exit 1; fi; \
	  if echo "$$STATUS" | grep -qi 'base infrastructure.*ready' || echo "$$STATUS" | grep -qi 'collab/PM/channels: ready' || ( echo "$$STATUS" | grep -q 'Court personas online: 7' && ( timeout 3s ./bin/aegis channel list >/dev/null 2>&1 || (sleep 1; timeout 3s ./bin/aegis channel list >/dev/null 2>&1) || (sleep 1; timeout 3s ./bin/aegis channel list >/dev/null 2>&1) ) ); then echo "   ✓ Base infrastructure ready (Network Boundary + Store + Web Portal registered; or Court 7 + channel list as secondary)"; else echo "   ✗ Base infrastructure not 'ready' (see status for attempted/registration issues)"; echo "$$STATUS"; exit 1; fi
	@./bin/aegis vm pools 2>/dev/null | grep -qE 'agent-pooled|memory-pooled' && echo "   ✓ Pre-warm pools present and claimable (aegis vm pools)" || (echo "   ✗ No pre-warm pools visible/claimable"; ./bin/aegis vm pools; exit 1)
	@ (./bin/aegis status 2>/dev/null; ./bin/aegis vm list 2>/dev/null || true) | grep -qE 'aegis-daemon-temp|daemon-temp-' && (echo "   ✗ Unexpected aegis-daemon-temp-* or daemon-temp components linger (see status/vm.list and logs)"; exit 1) || echo "   ✓ No unexpected aegis-daemon-temp-* components"
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
		echo "       (start daemon with: AEGIS_BOOT_TIMING=1 sudo -E ./bin/aegis start --foreground)"; \
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

# E2E-specific cleanup for reliable repeated runs of make test-e2e-llm / verify script
# (sockets, custom state dirs, test procs). Use after partial failures or to reset.
# Safe to run; uses sudo -n for root-owned artifacts from previous starts.
e2e-clean:
	@echo "=== E2E cleanup for reliable repeated runs (per testing-standards.md priority 1 + AGENTS.md) ==="
	-@sudo -n ./bin/aegis stop >/dev/null 2>&1 || true
	-@./bin/aegis stop >/dev/null 2>&1 || true
	@echo "  Waiting for daemon to stop (control socket; required before isolated E2E)..."
	@bash -c 'for i in $$(seq 1 20); do ./bin/aegis status 2>/dev/null | grep -q "daemon is not running" && exit 0; sudo -n ./bin/aegis stop 2>/dev/null || true; sleep 1; done; echo "  (warn: daemon may still be stopping; wait before FORCE_ISOLATED)"; exit 0'
	@echo "  Waiting for firecracker VMs to exit (graceful stop; do not use pkill -9 firecracker)..."
	@bash -c 'for i in $$(seq 1 15); do pgrep -x firecracker >/dev/null 2>&1 || exit 0; sleep 1; done; pgrep -x firecracker >/dev/null 2>&1 && echo "  (note: firecracker still running; sudo ./bin/aegis stop and retry e2e-clean)" || true'
	-@sudo -n rm -f $(HOME)/.aegis/hub.sock /tmp/aegis/hub-pmllm-e2e.sock 2>/dev/null || true
	-@sudo -n pkill -x aegis >/dev/null 2>&1 || true
	-@sudo -n pkill -x aegishub >/dev/null 2>&1 || true
	-@sudo -n pkill -f 'aegis start --foreground' >/dev/null 2>&1 || true
	-@sudo -n pkill -f 'aegishub start' >/dev/null 2>&1 || true
	-@sudo -n pkill -f 'aegis-daemon' >/dev/null 2>&1 || true
	-@sudo -n rm -f /tmp/aegis/hub-*.sock /tmp/aegis/hub-pmllm-e2e.sock ~/.aegis/hub.sock /tmp/aegis/daemon.pid >/dev/null 2>&1 || true
	-@rm -rf /tmp/aegis /tmp/aegis-pmllm-e2e /tmp/aegis-*verify /tmp/aegis-pm* /tmp/aegis-* 2>/dev/null || true
	-@sudo -n rm -rf /tmp/aegis /tmp/aegis-pmllm-e2e /tmp/aegis-*verify /tmp/aegis-pm* /tmp/aegis-* ~/.aegis/hub.sock /tmp/aegis/daemon.pid 2>/dev/null || true
	@ls -ld /tmp/aegis* ~/.aegis/hub.sock 2>/dev/null | cat || echo '  (no /tmp/aegis* or main hub.sock remaining)'
	@echo "✓ E2E custom state, sockets, test procs, and temp dirs cleaned."
	@echo "  Safe to re-run 'make test-e2e-llm' or the script (even after SIGINT/partial fail on real hw)."
	@echo "  Suggested next: AEGIS_DEFAULT_MODEL=llama3.2:3b sudo ./bin/aegis start --foreground"
	@echo "    # Then poll until healthy (real Firecracker boots take time):"
	@echo "    #   while ! make smoke >/dev/null 2>&1; do sleep 5; done; make smoke"
	@echo "    # Or just: make test-e2e-llm   (the script waits internally for Court==7 + base ready)"
	@echo "  (For full microVM/rootfs clean: make clean-microvms -- DANGER, only if needed for fresh images)"

# Run unit tests
test:
	go test ./...

# Run daemon integration tests
test-integration:
	go test -v -tags=integration ./cmd/aegis -run "TestDaemon|TestCLI|TestVersion|TestProcessCleaning|TestVMList|TestSocket" -count=1 -timeout 90s

# Run E2E tests in a real browser against the live daemon + microVMs (start daemon first)
test-e2e:
	bash scripts/run-playwright-e2e.sh e2e/chat.spec.js --project=chromium

# Contract-only E2E against the thin web-portal fixture (no daemon). For CI without Firecracker.
test-e2e-contract:
	AEGIS_E2E_FIXTURE=1 bash scripts/run-playwright-e2e.sh e2e/journeys.spec.js --project=chromium

# Real unmocked E2E exercising PM + LLM (Ollama via network-boundary) + channels exactly as a user would:
#   `aegis pm goal "..." --channel foo` then `aegis channel get foo` (or view in portal #channels).
# Uses isolated custom hub/state + short waits + AEGIS_DEFAULT_MODEL (defaults llama3.2:3b).
# Requires: make build, sudo -n for ./bin/aegis (per AGENTS.md), ollama running with the model.
# Includes explicit `./bin/aegis status` after start (before other tests), plus browser (Playwright)
# verification of the channels UI showing the PM post (not just CLI).
# This is the "no fixtures, hits real LLM" verification for the collaboration model.
test-e2e-llm:
	@echo "=== Real PM+LLM+Channels E2E (unmocked, user path via CLI pm goal + channel inspect + browser) ==="
	@echo "See scripts/verify-pm-llm-e2e.sh for details and success criteria."
	AEGIS_DEFAULT_MODEL="$${AEGIS_DEFAULT_MODEL:-llama3.2:3b}" bash scripts/verify-pm-llm-e2e.sh

# Fully isolated cold-start E2E (custom hub socket; stops main daemon first).
test-e2e-llm-isolated: e2e-clean
	@echo "=== Isolated PM+LLM+Channels E2E (FORCE_ISOLATED=1 after e2e-clean) ==="
	FORCE_ISOLATED=1 AEGIS_DEFAULT_MODEL="$${AEGIS_DEFAULT_MODEL:-llama3.2:3b}" bash scripts/verify-pm-llm-e2e.sh

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
			echo "  Hooks are additive and non-fatal (no breakage to daemon start/stop or make test)."; \
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
	@echo "  sudo ./bin/aegis start --foreground"
	@echo "  make smoke"
	@echo "  ./bin/aegis chat --headless \"Hello\""

# Help target
help:
	@echo "AegisClaw Build System"
	@echo ""
	@echo "Targets:"
	@echo "  make build              Build host binaries + microVM guest images (everything needed to start the daemon)"
	@echo "  make build-binaries     Build Go binaries only"
	@echo "  make build-microvms     Build microVM filesystems (NOPASSWD: scripts/create-firecracker-rootfs.sh)"
	@echo "  make setup              Onboarding helper (build + doctor) - Journey 01"
	@echo "  sudo ./bin/aegis start  Start the daemon (preferred per AGENTS.md)"
	@echo "  sudo ./bin/aegis start --foreground   Start daemon in foreground (debugging)"
	@echo "  make stop               Stop the daemon"
	@echo "  make status             Check daemon status"
	@echo "  make doctor             Run health checks"
	@echo "  make smoke              Quick smoke test after start (CLI + portal + teams)"
	@echo "  make test               Run unit tests"
	@echo "  make test-integration   Run daemon integration tests"
	@echo "  make test-e2e           Browser E2E vs real daemon + microVMs (requires running daemon)"
	@echo "  make test-e2e-contract  Thin-portal contract tests only (no daemon; AEGIS_E2E_FIXTURE=1)"
	@echo "  make test-e2e-llm       Real unmocked PM+LLM+channels E2E (CLI pm goal + channel get + browser UI + status check; hits Ollama, no fixtures; see script)"
	@echo "  make test-e2e-llm-isolated  Same as test-e2e-llm but FORCE_ISOLATED=1 after e2e-clean (clean cold-start gate)"
	@echo "  make test-tcb           TCB-specific tests (7.5)"
	@echo "  make test-chaos         Chaos/restart tests (7.7, requires AEGIS_CHAOS=1)"
	@echo "  make sbom               SBOM + supply-chain (7.8: CycloneDX or fallback + cosign hooks)"
	@echo "  make clean              Remove build artifacts (binaries, sbom, test results, etc.)"
	@echo "  make clean-microvms     ⚠ DANGER: Delete microVM images (YES=1 skips prompt; /opt via scripts/clean-firecracker-rootfs.sh)"
	@echo "  make help               Show this help message"
	@echo ""
	@echo "Setup:"
	@echo "  1. Install dependencies: go mod download"
	@echo "  2. Build everything (binaries + fresh guest VM images): make build"
	@echo "     (After source changes to web-portal, agents, store, etc. this is the"
	@echo "      single command that should give you an up-to-date system.)"
	@echo "  3. Start daemon: sudo ./bin/aegis start --foreground"
	@echo "  4. (Optional but recommended) Verify: make smoke"
	@echo ""
	@echo "Documentation:"
	@echo "  See README.md for detailed setup instructions"
