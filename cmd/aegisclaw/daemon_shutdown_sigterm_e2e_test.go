//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"testing"
	"time"
)

// TestSignalShutdownSIGTERM_TerminatesTrackedVMs_E2E is DB-01: subprocess receives
// SIGTERM and must invoke the same Stop/Delete sequence as the daemon signal path.
func TestSignalShutdownSIGTERM_TerminatesTrackedVMs_E2E(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	if runtime.GOOS == "windows" {
		t.Skip("unix signals only")
	}

	// Extra-safe hermetic config setup for both parent and child processes.
	// This test spawns a full re-execution of the test binary; without this,
	// the child can hit the same "create default config on clean HOME" path
	// that has repeatedly caused OOMs in CI during `make test`.
	setupTestConfig(t)

	resultPath := filepath.Join(t.TempDir(), "result.json")
	readyPath := filepath.Join(t.TempDir(), "ready")

	if os.Getenv("AEGISCLAW_SIGVM_E2E_CHILD") == "1" {
		resultPath = os.Getenv("AEGISCLAW_SIGVM_E2E_RESULT")
		readyPath = os.Getenv("AEGISCLAW_SIGVM_E2E_READY")
		rec := &recordingMicroVM{}
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM)
		if err := os.WriteFile(readyPath, []byte("ok"), 0600); err != nil {
			t.Fatalf("ready: %v", err)
		}
		<-sig
		terminateManagedHubAndStoreVMs(context.Background(), rec, "hub-e2e", "store-e2e")
		b, err := json.Marshal(map[string][]string{"stops": rec.stops, "deletes": rec.deletes})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(resultPath, b, 0600); err != nil {
			t.Fatalf("write result: %v", err)
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSignalShutdownSIGTERM_TerminatesTrackedVMs_E2E", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"AEGISCLAW_SIGVM_E2E_CHILD=1",
		"AEGISCLAW_SIGVM_E2E_RESULT="+resultPath,
		"AEGISCLAW_SIGVM_E2E_READY="+readyPath,
	)

	// Capture the child's output. This is critical for debugging why the
	// child exits with non-zero status (currently causing "wait: exit status 2").
	var childOutput bytes.Buffer
	cmd.Stdout = &childOutput
	cmd.Stderr = &childOutput

	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err := <-waitErr:
		if err != nil {
			t.Logf("child process output:\n%s", childOutput.String())
			t.Fatalf("wait: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Logf("child process output (before kill):\n%s", childOutput.String())
		t.Fatal("child did not exit")
	}

	raw, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var out struct {
		Stops   []string `json:"stops"`
		Deletes []string `json:"deletes"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	wantStops := []string{"hub-e2e", "store-e2e"}
	wantDeletes := []string{"hub-e2e", "store-e2e"}
	if !slices.Equal(out.Stops, wantStops) {
		t.Fatalf("stops: got %#v want %#v", out.Stops, wantStops)
	}
	if !slices.Equal(out.Deletes, wantDeletes) {
		t.Fatalf("deletes: got %#v want %#v", out.Deletes, wantDeletes)
	}
}
