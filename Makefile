.PHONY: build vet test test-short test-integration test-all fuzz

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

test-short:
	go test -short ./...

test-integration:
	go test -tags=integration ./cmd/aegisclaw/ -run 'Integration|Lifecycle|Journey' -v

test-all: test test-integration

# Fuzz testing (Go 1.18+)
fuzz:
	@echo "Running fuzz tests..."
	go test -fuzz=Fuzz ./cmd/aegisclaw/... -fuzztime=30s || true
