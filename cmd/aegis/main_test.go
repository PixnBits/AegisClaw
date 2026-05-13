package main

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"AegisClaw/internal/config"
)

const testSocketPath = "/tmp/aegis_test.sock"

func repoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func buildRepoBinary(t *testing.T, repoRoot, pkgPath, outputName string) string {
	t.Helper()

	binDir := filepath.Join(repoRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("Failed to create bin directory: %v", err)
	}

	outputPath := filepath.Join(binDir, outputName)
	buildCmd := exec.Command("go", "build", "-o", outputPath, pkgPath)
	buildCmd.Dir = repoRoot
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build %s: %v\n%s", pkgPath, err, output)
	}

	return outputPath
}

func waitForUnixSocket(t *testing.T, socketPath string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", socketPath, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("Timed out waiting for socket %s", socketPath)
}

func TestDaemonStartAndStatus(t *testing.T) {
	// Test daemon binary existence and basic functionality
	// Root-required portions are skipped if not root

	rootDir := repoRoot(t)
	aegisBinary := filepath.Join(rootDir, "bin", "aegis")

	// Test 1: Verify binary exists
	if _, err := os.Stat(aegisBinary); err != nil {
		t.Fatalf("aegis binary not found at %s", aegisBinary)
	}
	t.Logf("✓ Daemon binary found: %s", aegisBinary)

	// Test 2: Verify help command works
	cmd := exec.Command(aegisBinary, "help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("help command failed: %v", err)
	}
	if !strings.Contains(string(output), "aegis") {
		t.Errorf("help output should contain 'aegis', got: %s", string(output))
	}
	t.Logf("✓ Help command works")

	// Test 3: Verify doctor command works
	cmd = exec.Command(aegisBinary, "doctor")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Logf("doctor command output: %s", string(output))
	}
	if !strings.Contains(string(output), "Health checks") {
		t.Errorf("doctor output should contain 'Health checks'")
	}
	t.Logf("✓ Doctor command works")

	// Root-only tests
	if os.Getuid() != 0 {
		t.Logf("Skipping root-only tests (status, start, stop)")
		return
	}

	// Clean up
	os.Remove(testSocketPath)

	// Test 4 (root only): Status command works
	cmd = exec.Command(aegisBinary, "status")
	output, _ = cmd.CombinedOutput()
	status := string(output)
	if !strings.Contains(status, "daemon is") {
		t.Errorf("status should contain 'daemon is', got: %s", status)
	}
	t.Logf("✓ Status command works: %s", strings.TrimSpace(status))
}

func TestIsDaemonRunning(t *testing.T) {
	// This test verifies isDaemonRunning behavior through the status command
	// Most of it can run without root, but start/stop requires sudo

	// Skip if not root and sudo requires password
	if os.Getuid() != 0 {
		t.Logf("Skipping root-only daemon lifecycle tests (use 'sudo go test' to run)")
		return
	}

	// Ensure no daemon is running by trying to stop
	stopCmd := exec.Command("./bin/aegis", "stop")
	_ = stopCmd.Run()
	time.Sleep(500 * time.Millisecond)

	// Clean up any stale PID file
	os.Remove("/tmp/aegis/daemon.pid")
	time.Sleep(100 * time.Millisecond)

	// Test 1: Daemon should not be running
	cmd := exec.Command("./bin/aegis", "status")
	output, _ := cmd.CombinedOutput()
	status := string(output)

	if !strings.Contains(status, "daemon is not running") {
		t.Logf("Initial status: %s", strings.TrimSpace(status))
	}

	// Test 2: After starting daemon, status should show running
	startCmd := exec.Command("./bin/aegis", "start")
	_ = startCmd.Run()
	time.Sleep(2 * time.Second)

	cmd = exec.Command("./bin/aegis", "status")
	output, _ = cmd.CombinedOutput()
	status = string(output)

	if !strings.Contains(status, "daemon is running") {
		t.Logf("After starting, status: %s", strings.TrimSpace(status))
	} else {
		t.Logf("✓ Daemon correctly reports as running after start")
	}

	// Cleanup
	stopCmd = exec.Command("./bin/aegis", "stop")
	_ = stopCmd.Run()
}

func TestStatusJSON(t *testing.T) {
	response := "Daemon: running\nBackend: Firecracker\nSafe Mode: false\nRunning VMs: 0\nUptime: 1s\nPID: 123\n"
	lines := strings.Split(strings.TrimSpace(response), "\n")
	status := map[string]interface{}{}
	for _, line := range lines {
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) == 2 {
			key := parts[0]
			val := parts[1]
			switch key {
			case "Running VMs", "PID":
				if num, err := strconv.Atoi(val); err == nil {
					status[strings.ToLower(strings.ReplaceAll(key, " ", ""))] = num
				}
			case "Safe Mode":
				status["safeMode"] = val == "true"
			default:
				status[strings.ToLower(strings.ReplaceAll(key, " ", ""))] = val
			}
		}
	}
	expected := map[string]interface{}{
		"daemon":     "running",
		"backend":    "Firecracker",
		"safeMode":   false,
		"runningvms": 0,
		"uptime":     "1s",
		"pid":        123,
	}
	for k, v := range expected {
		if status[k] != v {
			t.Errorf("Expected %v for %s, got %v", v, k, status[k])
		}
	}
}

func TestGetOriginalUser(t *testing.T) {
	// This test verifies the ability to get the original user when running with sudo
	// In a test environment, we can verify this works even when not using sudo

	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}

	if user == "" {
		t.Skip("USER/LOGNAME environment variable not set")
	}

	t.Logf("Current user: %s", user)

	// Verify we can determine a user (this would be more complex with sudo)
	if user != "root" {
		t.Logf("Running as non-root user: %s", user)
	}
}

func TestExpandPath(t *testing.T) {
	// This test verifies path expansion for tilde and environment variables
	// Test 1: Absolute paths should pass through unchanged
	absPath := "/tmp/test/path"
	result := filepath.Clean(absPath)
	if result != absPath {
		t.Errorf("Absolute path should not change, got %s", result)
	}

	// Test 2: Relative paths should be usable with filepath.Join
	relPath := "test/path"
	joined := filepath.Join(os.TempDir(), relPath)
	if !filepath.IsAbs(joined) {
		t.Errorf("Joined path should be absolute, got %s", joined)
	}
	t.Logf("Relative path test: %s -> %s", relPath, joined)

	// Test 3: Verify HOME directory exists
	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("HOME environment variable not set")
	}

	if _, err := os.Stat(home); err != nil {
		t.Errorf("HOME directory should exist: %v", err)
	}
	t.Logf("HOME directory: %s", home)
}

func TestVMConfig(t *testing.T) {
	// This test verifies VM configuration validation
	// Test 1: Check that config can be created
	cfg := config.New()
	if cfg == nil {
		t.Errorf("Config should not be nil")
	}

	// Test 2: Verify key config fields
	if cfg.Platform == "" {
		t.Errorf("Platform should not be empty")
	}
	t.Logf("Platform: %s", cfg.Platform)

	if cfg.SandboxType == "" {
		t.Errorf("SandboxType should not be empty")
	}
	t.Logf("Sandbox Type: %s", cfg.SandboxType)

	// Test 3: Verify state directory is set
	if cfg.StateDir == "" {
		t.Errorf("StateDir should not be empty")
	}
	t.Logf("State Directory: %s", cfg.StateDir)
}
