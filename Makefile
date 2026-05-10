.PHONY: build build-binaries clean test

# Build all binaries
build: build-binaries

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

# Clean build artifacts
clean:
	rm -rf bin/

# Run tests
test:
	go test ./...

# Run E2E tests
test-e2e:
	npm test