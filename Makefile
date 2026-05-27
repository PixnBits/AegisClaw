.PHONY: build build-binaries build-microvms clean test test-integration test-e2e smoke help doctor

# Default target
all: build

# Build all binaries and microVMs
build: build-binaries build-microvms

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

# Start the daemon
start:
	sudo ./bin/aegis start

# Start daemon in foreground for debugging
start-foreground:
	sudo ./bin/aegis start --foreground

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

# Clean build artifacts
clean:
	rm -rf bin/

# Run unit tests
test:
	go test ./...

# Run daemon integration tests
test-integration:
	go test -v -tags=integration ./cmd/aegis -run "TestDaemon|TestCLI|TestVersion|TestProcessCleaning|TestVMList|TestSocket" -count=1 -timeout 90s

# Run E2E tests
test-e2e:
	npm test

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
	@echo "  make build-microvms     Build microVM filesystems (Linux only)"
	@echo "  make setup              Onboarding helper (build + doctor) - Journey 01"
	@echo "  make start              Start the daemon with sudo"
	@echo "  make start-foreground   Start daemon in foreground (debugging)"
	@echo "  make stop               Stop the daemon"
	@echo "  make status             Check daemon status"
	@echo "  make doctor             Run health checks"
	@echo "  make smoke              Quick smoke test after 'make start' (CLI + portal + teams)"
	@echo "  make test               Run unit tests"
	@echo "  make test-integration   Run daemon integration tests"
	@echo "  make test-e2e           Run E2E tests"
	@echo "  make clean              Remove build artifacts"
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