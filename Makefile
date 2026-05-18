# Test targets

.PHONY: test test-integration test-all

# Run normal (fast) tests - used in CI by default
test:
	go test ./... -count=1 -timeout 120s

# Run integration tests (richer lifecycle, containment, etc.)
# These require the 'integration' build tag
test-integration:
	go test -tags=integration ./cmd/aegisclaw/ -run 'Lifecycle|Integration' -count=1 -v -timeout 180s

# Run both normal and integration tests
test-all: test test-integration

# Fuzz testing (future)
# fuzz:
#	go test -fuzz=Fuzz ./cmd/aegisclaw/...
