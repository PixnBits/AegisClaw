.PHONY: build build-static vet test test-short test-integration test-all fuzz

build:
	go build ./...

# Phase 4: Fully static binary build (CGO_ENABLED=0 + static ldflags).
# Produces a binary with no dynamic library dependencies, satisfying
# host-daemon.md "Static Binary" requirement.
build-static:
	CGO_ENABLED=0 go build -ldflags '-extldflags "-static"' -o aegisclaw ./cmd/aegisclaw
ifeq ($(shell uname -s),Linux)
	@file aegisclaw 2>/dev/null | grep -q "statically linked" || (echo "ERROR: binary is not statically linked" && exit 1)
	@echo "Static binary verified: aegisclaw"
else
	@echo "Static binary built: aegisclaw (static verification via 'file' only supported on Linux)"
endif

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
	go test -fuzz=Fuzz ./cmd/aegisclaw/... -fuzztime=30s
