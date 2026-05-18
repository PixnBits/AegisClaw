# Fuzz testing (Go 1.18+)
fuzz:
	@echo "Running fuzz tests..."
	go test -fuzz=Fuzz ./cmd/aegisclaw/... -fuzztime=30s || true
