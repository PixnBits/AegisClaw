# AegisClaw Makefile

BINARY := aegisclaw

.PHONY: build build-static clean

build:
	go build -o $(BINARY) ./cmd/aegisclaw

# Phase 4.4: Enforce static binary compilation
build-static:
	CGO_ENABLED=0 go build -ldflags "-s -w" -o $(BINARY) ./cmd/aegisclaw
	echo "Static binary built: $(BINARY)"

clean:
	rm -f $(BINARY)
