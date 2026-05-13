.PHONY: build build-binaries build-microvms clean test test-integration test-e2e help doctor

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

# Help target
help:
	@echo "AegisClaw Build System"
	@echo ""
	@echo "Targets:"
	@echo "  make build              Build binaries and microVMs"
	@echo "  make build-binaries     Build Go binaries only"
	@echo "  make build-microvms     Build microVM filesystems (Linux only)"
	@echo "  make start              Start the daemon with sudo"
	@echo "  make start-foreground   Start daemon in foreground (debugging)"
	@echo "  make stop               Stop the daemon"
	@echo "  make status             Check daemon status"
	@echo "  make doctor             Run health checks"
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
	@echo ""
	@echo "Documentation:"
	@echo "  See README.md for detailed setup instructions"