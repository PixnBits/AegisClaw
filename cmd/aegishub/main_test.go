package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"AegisClaw/internal/hublease"

	"github.com/mdlayher/vsock"
)

// NOTE (Phase 1.1c): AegisHub now also listens on vsock port 9999 (when available)
// for real Firecracker guest microVMs (Agent Runtime + Memory VM).
// The existing unix-socket roundtrip tests continue to cover the shared handleConnection logic.
// Vsock-specific integration is exercised when running inside actual microVMs (see AGENTS.md + build-microvms).

func waitUnixReady(t *testing.T, sock string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	var dialErr error
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("unix", sock, 50*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		dialErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("hub not accepting on %s: %v", sock, dialErr)
}
