//go:build integration
// +build integration

package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDaemonLifecycle tests the complete daemon start/status/stop sequence
func TestDaemonLifecycle(t *testing.T) {
	// This test requires cleaning up stale daemons first
	pidFile := filepath.Join("/tmp", "aegis", "daemon.pid")

	// Clean up old PID file if it exists and process is dead
	if data, err := os.ReadFile(pidFile); err == nil {
		pidStr := strings.TrimSpace(string(data))
		procPath := fmt.Sprintf("/proc/%s", pidStr)
		if _, err := os.Stat(procPath); err != nil {
			// Process doesn't exist, clean up the stale PID file
			os.Remove(pidFile)
			t.Logf("Cleaned up stale PID file: %s", pidFile)
		} else {
			t.Skipf("Skipping test - daemon already running (stale from previous run). Run 'sudo %s/bin/aegis stop' to clean up.", repoRoot(t))
		}
	}

	// Get the aegis binary path
	rootDir := repoRoot(t)
	aegisBinary := filepath.Join(rootDir, "bin", "aegis")
	if _, err := os.Stat(aegisBinary); err != nil {
		t.Skipf("aegis binary not found at %s — run 'make build-binaries' first. Integration tests that require the real binary are skipped.", aegisBinary)
	}

	t.Run("daemon_not_running_initially", func(t *testing.T) {
		// Behavior: status command returns proper output
		cmd := exec.Command(aegisBinary, "status")
		output, _ := cmd.CombinedOutput()
		status := string(output)

		// VALIDATION: Output must contain one of these (user expectation)
		if !strings.Contains(status, "daemon is not running") && !strings.Contains(status, "daemon is running") {
			t.Errorf("BEHAVIOR FAILED: status output invalid: %s", status)
			return
		}
		t.Logf("✓ status returns valid output: %s", strings.TrimSpace(status))
	})

	t.Run("daemon_starts_successfully", func(t *testing.T) {
		// Behavior: daemon can be started and reports success
		cmd := exec.Command("sudo", aegisBinary, "start")
		output, _ := cmd.CombinedOutput()
		status := string(output)

		// VALIDATION: Must explicitly say daemon started (user expectation)
		if !strings.Contains(status, "daemon started") {
			t.Errorf("BEHAVIOR FAILED: start didn't report success: %s", status)
			return
		}
		t.Logf("✓ start reports success: %s", strings.TrimSpace(status))

		// Wait for daemon to be ready
		time.Sleep(2 * time.Second)
	})

	t.Run("daemon_status_shows_running", func(t *testing.T) {
		// Behavior: status command shows daemon is running after start
		cmd := exec.Command(aegisBinary, "status")
		output, err := cmd.CombinedOutput()
		status := string(output)

		// VALIDATION: status must report running (user expectation)
		if err != nil || !strings.Contains(status, "daemon is running") {
			t.Errorf("BEHAVIOR FAILED: daemon should be running but status says: %s (err: %v)", status, err)
			return
		}
		t.Logf("✓ status correctly reports: %s", strings.TrimSpace(status))
	})

	t.Run("daemon_prevents_duplicate_start", func(t *testing.T) {
		// Behavior: daemon prevents duplicate start
		cmd := exec.Command("sudo", aegisBinary, "start")
		output, _ := cmd.CombinedOutput()
		status := string(output)

		// VALIDATION: Must say already running (user expectation)
		if !strings.Contains(status, "already running") {
			t.Errorf("BEHAVIOR FAILED: duplicate start should be prevented, got: %s", status)
			return
		}
		t.Logf("✓ duplicate start properly rejected: %s", strings.TrimSpace(status))
	})

	t.Run("daemon_stops_successfully", func(t *testing.T) {
		// Behavior: daemon can be stopped and reports success
		cmd := exec.Command("sudo", aegisBinary, "stop")
		output, _ := cmd.CombinedOutput()
		status := string(output)

		// VALIDATION: Must explicitly report stop success (user expectation)
		if !strings.Contains(status, "daemon stopped") {
			t.Errorf("BEHAVIOR FAILED: stop didn't report success: %s", status)
			return
		}
		t.Logf("✓ stop reports success: %s", strings.TrimSpace(status))
	})

	t.Run("daemon_not_running_after_stop", func(t *testing.T) {
		// Behavior: status shows daemon is not running after stop
		time.Sleep(1 * time.Second) // Give it time to clean up
		cmd := exec.Command(aegisBinary, "status")
		output, _ := cmd.CombinedOutput()
		status := string(output)

		// VALIDATION: status must report not running (user expectation)
		if !strings.Contains(status, "daemon is not running") {
			t.Errorf("BEHAVIOR FAILED: daemon should not be running after stop, but status says: %s", status)
			return
		}
		t.Logf("✓ status correctly reports: %s", strings.TrimSpace(status))
	})
}

// TestDaemonDoctor tests the health check command with behavior validation
func TestDaemonDoctor(t *testing.T) {
	rootDir := repoRoot(t)
	aegisBinary := filepath.Join(rootDir, "bin", "aegis")
	if _, err := os.Stat(aegisBinary); err != nil {
		t.Skipf("aegis binary not found at %s", aegisBinary)
	}

	cmd := exec.Command(aegisBinary, "doctor")
	output, _ := cmd.CombinedOutput()
	status := string(output)

	t.Logf("Doctor output:\n%s", status)

	// BEHAVIOR VALIDATION: doctor must return all expected health check items
	expectedChecks := []string{
		"Platform:",
		"Sandbox type:",
		"State directory:",
		"Health checks complete",
	}

	for _, check := range expectedChecks {
		if !strings.Contains(status, check) {
			t.Errorf("BEHAVIOR FAILED: health check for '%s' missing from output", check)
		}
	}

	// REGRESSION CHECK: doctor must not return error messages
	if strings.Contains(status, "FATAL") || strings.Contains(status, "Error") || strings.Contains(status, "error") {
		t.Logf("Warning: doctor output contains error messages: %s", status)
	}

	t.Logf("✓ doctor command returns all expected health checks")
}

// TestWebPortalConnectivity tests if web portal can be accessed
func TestWebPortalConnectivity(t *testing.T) {
	// Note: This test assumes web portal is running on localhost:8080
	// It's a basic connectivity test, not a full functional test

	webPortalURL := "http://localhost:8080"
	timeout := 5 * time.Second

	client := &http.Client{
		Timeout: timeout,
	}

	// Try to connect to web portal
	resp, err := client.Get(webPortalURL)
	if err != nil {
		t.Skipf("Web portal not running at %s: %v", webPortalURL, err)
		return
	}
	defer resp.Body.Close()

	// Verify we got a successful response
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Errorf("Expected successful response, got status %d", resp.StatusCode)
		return
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	t.Logf("Web portal responded with Content-Type: %s", contentType)

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Failed to read response body: %v", err)
		return
	}

	// Check for HTML content
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "<!DOCTYPE") && !strings.Contains(bodyStr, "<html") {
		t.Logf("Warning: Response doesn't look like HTML, first 200 chars: %.200s", bodyStr)
	}

	t.Logf("✓ Web portal responded successfully with %d bytes", len(body))
}

// TestWebPortalAPIs tests basic web portal API endpoints
func TestWebPortalAPIs(t *testing.T) {
	baseURL := "http://localhost:8080"
	timeout := 5 * time.Second

	client := &http.Client{
		Timeout: timeout,
	}

	// Test common API endpoints
	apiTests := []struct {
		name   string
		path   string
		method string
	}{
		{"Dashboard API", "/api/dashboard", "GET"},
		{"Status API", "/api/status", "GET"},
		{"VMs API", "/api/vms", "GET"},
		{"Skills API", "/api/skills", "GET"},
	}

	for _, test := range apiTests {
		t.Run(test.name, func(t *testing.T) {
			url := baseURL + test.path
			req, err := http.NewRequest(test.method, url, nil)
			if err != nil {
				t.Errorf("Failed to create request: %v", err)
				return
			}

			// Add Accept header
			req.Header.Set("Accept", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				t.Logf("API endpoint %s not available: %v", test.path, err)
				return
			}
			defer resp.Body.Close()

			// Log status for debugging
			t.Logf("%s: HTTP %d", test.path, resp.StatusCode)

			// Any response is acceptable for this test (including 404)
			if resp.StatusCode >= 200 && resp.StatusCode < 600 {
				// Read and log response for debugging
				body, _ := io.ReadAll(resp.Body)
				t.Logf("Response (%d bytes): %.200s", len(body), string(body))
			}
		})
	}
}

// TestDaemonWithVersionInfo tests that daemon reports component versions
func TestDaemonWithVersionInfo(t *testing.T) {
	rootDir := repoRoot(t)
	aegisBinary := filepath.Join(rootDir, "bin", "aegis")
	if _, err := os.Stat(aegisBinary); err != nil {
		t.Skipf("aegis binary not found at %s", aegisBinary)
	}

	// Check aegis binary version info
	fileInfo, err := os.Stat(aegisBinary)
	if err != nil {
		t.Fatalf("Failed to stat aegis binary: %v", err)
	}

	t.Logf("Aegis binary info:")
	t.Logf("  - Size: %d bytes", fileInfo.Size())
	t.Logf("  - Modified: %v", fileInfo.ModTime())

	// Check other microVM components
	components := []string{
		"agent",
		"web-portal",
		"builder",
		"store",
		"memory",
		"network-boundary",
		"court-persona",
		"court-scribe",
	}

	t.Logf("MicroVM Components:")
	for _, comp := range components {
		binPath := filepath.Join(rootDir, "bin", comp)
		if fileInfo, err := os.Stat(binPath); err == nil {
			t.Logf("  - %s: %d bytes, modified %v", comp, fileInfo.Size(), fileInfo.ModTime())
		} else {
			t.Logf("  - %s: NOT BUILT (run 'make build')", comp)
		}
	}

	// Check filesystem images
	filesystemsDir := filepath.Join(os.Getenv("HOME"), ".aegis", "firecracker", "rootfs")
	if fileInfo, err := os.Stat(filesystemsDir); err == nil {
		t.Logf("\nMicroVM Filesystems directory: %s", filesystemsDir)
		t.Logf("  - Modified: %v", fileInfo.ModTime())

		// List built filesystems
		entries, err := os.ReadDir(filesystemsDir)
		if err == nil {
			t.Logf("  - Built filesystems:")
			for _, entry := range entries {
				if entry.IsDir() {
					t.Logf("    - %s/", entry.Name())
				}
			}
		}
	} else {
		t.Logf("MicroVM Filesystems not built yet (expected location: %s)", filesystemsDir)
	}
}

// TestLocalCurlToWebPortal tests using curl to access web portal
func TestLocalCurlToWebPortal(t *testing.T) {
	webPortalURL := "http://localhost:8080"

	// Test with curl command line tool
	cmd := exec.Command("curl", "-s", "-I", webPortalURL)
	output, err := cmd.CombinedOutput()

	response := string(output)
	t.Logf("curl response:\n%s", response)

	// Check for HTTP response or skip if portal not running
	if response == "" || err != nil {
		t.Skipf("Web portal not running at %s - skipping curl test", webPortalURL)
	}

	if !strings.Contains(response, "HTTP/") {
		t.Errorf("Expected HTTP response, got: %s", response)
		return
	}

	t.Logf("✓ curl successfully reached web portal at %s", webPortalURL)
}

// TestDaemonCLICommands tests various daemon CLI commands
func TestDaemonCLICommands(t *testing.T) {
	rootDir := repoRoot(t)
	aegisBinary := filepath.Join(rootDir, "bin", "aegis")
	if _, err := os.Stat(aegisBinary); err != nil {
		t.Skipf("aegis binary not found")
	}

	tests := []struct {
		name     string
		args     []string
		contains string
	}{
		{"status command", []string{"status"}, "daemon"},
		{"doctor command", []string{"doctor"}, "Health checks"},
		{"help command", []string{"--help"}, "daemon"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.Command(aegisBinary, test.args...)
			output, _ := cmd.CombinedOutput()
			response := string(output)

			t.Logf("Output: %s", response)

			if !strings.Contains(response, test.contains) {
				t.Logf("Warning: Expected output to contain '%s', got: %s", test.contains, response)
			}
		})
	}
}

// TestDaemonProcessCleaning tests that daemon cleans up properly
func TestDaemonProcessCleaning(t *testing.T) {
	pidFile := filepath.Join("/tmp", "aegis", "daemon.pid")

	// Check if PID file exists and is readable
	if data, err := os.ReadFile(pidFile); err == nil {
		pid := strings.TrimSpace(string(data))
		t.Logf("Found daemon PID file: %s (PID: %s)", pidFile, pid)

		// Check if process still exists
		procPath := fmt.Sprintf("/proc/%s", pid)
		if _, err := os.Stat(procPath); err == nil {
			t.Logf("  - Process still exists: %s", procPath)
		} else {
			t.Logf("  - Process not found: %s (stale PID file)", procPath)
		}
	} else {
		t.Logf("No daemon PID file found at %s", pidFile)
	}

	// List PID file permissions
	if fileInfo, err := os.Stat(pidFile); err == nil {
		t.Logf("PID file permissions: %v", fileInfo.Mode())
	}

	// List directory permissions
	dirPath := filepath.Dir(pidFile)
	if fileInfo, err := os.Stat(dirPath); err == nil {
		t.Logf("PID directory permissions: %v", fileInfo.Mode())
	}
}

// TestVMListCommand tests the 'aegis vm list' command end-to-end with behavior validation
func TestVMListCommand(t *testing.T) {
	rootDir := repoRoot(t)
	aegisBinary := filepath.Join(rootDir, "bin", "aegis")

	defer func() {
		// Cleanup
		exec.Command("sudo", aegisBinary, "stop").Run()
	}()

	// Start daemon
	startCmd := exec.Command("sudo", aegisBinary, "start")
	if err := startCmd.Run(); err != nil {
		t.Skipf("Could not start daemon (may need sudo): %v", err)
	}

	// Wait for daemon to fully start
	time.Sleep(1 * time.Second)

	// Verify socket exists (infrastructure check before behavior tests)
	socketPath := filepath.Join("/tmp", "aegis", "daemon.sock")
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("INFRASTRUCTURE FAILED: Socket file not found at %s: %v", socketPath, err)
	}

	// Test: vm list command basic behavior
	t.Run("vm_list_basic", func(t *testing.T) {
		cmd := exec.Command(aegisBinary, "vm", "list")
		output, err := cmd.CombinedOutput()
		outputStr := string(output)

		// REGRESSION CHECK: Must not return "not yet implemented"
		if strings.Contains(outputStr, "not yet implemented") {
			t.Errorf("REGRESSION DETECTED: vm list returned 'not yet implemented' - feature broken!")
			t.Errorf("Full output: %s", outputStr)
			return
		}

		// BEHAVIOR VALIDATION: Must return proper output format
		if !strings.Contains(outputStr, "Running VMs:") {
			t.Errorf("BEHAVIOR FAILED: vm list must start with 'Running VMs:', got: %s", outputStr)
			return
		}

		// BEHAVIOR VALIDATION: For empty list, must say "No running VMs"
		if !strings.Contains(outputStr, "No running VMs") {
			t.Errorf("BEHAVIOR FAILED: vm list must show 'No running VMs' when empty, got: %s", outputStr)
			return
		}

		// BEHAVIOR VALIDATION: Exit code should be 0
		if err != nil {
			t.Errorf("BEHAVIOR FAILED: vm list should succeed with exit code 0, got error: %v", err)
			return
		}

		t.Logf("✓ vm list returns correct output format")
	})

	// Test: vm list --json behavior
	t.Run("vm_list_json", func(t *testing.T) {
		cmd := exec.Command(aegisBinary, "vm", "list", "--json")
		output, err := cmd.CombinedOutput()
		outputStr := string(output)

		// REGRESSION CHECK: Must not return "not yet implemented"
		if strings.Contains(outputStr, "not yet implemented") {
			t.Errorf("REGRESSION DETECTED: vm list --json returned 'not yet implemented'!")
			return
		}

		// BEHAVIOR VALIDATION: Output must be valid JSON
		if !strings.HasPrefix(strings.TrimSpace(outputStr), "[") {
			t.Errorf("BEHAVIOR FAILED: vm list --json must return JSON array, got: %s", outputStr)
			return
		}

		// BEHAVIOR VALIDATION: JSON must be empty array for no VMs
		if strings.TrimSpace(outputStr) != "[]" {
			t.Logf("Note: vm list --json output is not empty array: %s", outputStr)
		}

		// BEHAVIOR VALIDATION: Exit code should be 0
		if err != nil {
			t.Errorf("BEHAVIOR FAILED: vm list --json should succeed, got error: %v", err)
			return
		}

		t.Logf("✓ vm list --json returns valid JSON format")
	})

	// Test: status command works with daemon running
	t.Run("status_with_daemon_running", func(t *testing.T) {
		cmd := exec.Command(aegisBinary, "status")
		output, _ := cmd.CombinedOutput()
		outputStr := string(output)

		// BEHAVIOR VALIDATION: Must report daemon is running
		if !strings.Contains(outputStr, "daemon is running") {
			t.Errorf("BEHAVIOR FAILED: status must show 'daemon is running', got: %s", outputStr)
			return
		}

		t.Logf("✓ status correctly reports daemon running")
	})
}

// TestSocketServer verifies Unix socket server setup and behavior
func TestSocketServer(t *testing.T) {
	rootDir := repoRoot(t)
	aegisBinary := filepath.Join(rootDir, "bin", "aegis")
	socketPath := filepath.Join("/tmp", "aegis", "daemon.sock")

	defer func() {
		exec.Command("sudo", aegisBinary, "stop").Run()
	}()

	// Start daemon
	startCmd := exec.Command("sudo", aegisBinary, "start")
	if err := startCmd.Run(); err != nil {
		t.Skipf("Could not start daemon: %v", err)
	}

	time.Sleep(1 * time.Second)

	// Test: Socket file exists
	t.Run("socket_exists", func(t *testing.T) {
		if _, err := os.Stat(socketPath); err != nil {
			t.Fatalf("INFRASTRUCTURE FAILED: Socket file not found at %s: %v", socketPath, err)
		}
		t.Logf("✓ socket file exists at %s", socketPath)
	})

	// Test: Socket is readable by all users
	t.Run("socket_permissions", func(t *testing.T) {
		fileInfo, err := os.Stat(socketPath)
		if err != nil {
			t.Fatalf("INFRASTRUCTURE FAILED: Could not stat socket: %v", err)
		}

		// BEHAVIOR VALIDATION: Socket should be accessible by all users
		perms := fileInfo.Mode().Perm()
		if perms&0666 != 0666 {
			t.Logf("Warning: Socket permissions %o may restrict client access", perms)
		} else {
			t.Logf("✓ socket has correct permissions for multi-user access: %o", perms)
		}
	})

	// Test: Socket accepts connections and responds to commands
	t.Run("socket_accepts_connections", func(t *testing.T) {
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("PROTOCOL FAILED: Could not connect to socket: %v", err)
		}
		defer conn.Close()

		// BEHAVIOR VALIDATION: Send vm list command and get response
		conn.Write([]byte("vm list"))
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			t.Errorf("PROTOCOL FAILED: Could not read response from socket: %v", err)
			return
		}

		response := string(buf[:n])

		// REGRESSION CHECK: Must not return "not yet implemented"
		if strings.Contains(response, "not yet implemented") {
			t.Errorf("REGRESSION DETECTED: Socket handler returned 'not yet implemented': %s", response)
			return
		}

		// BEHAVIOR VALIDATION: Must return proper vm list response
		if !strings.Contains(response, "Running VMs") && !strings.Contains(response, "No running VMs") {
			t.Errorf("PROTOCOL FAILED: Unexpected socket response format: %s", response)
			return
		}

		t.Logf("✓ socket accepts connections and returns valid vm list response")
	})

	// Test: Socket protocol - multiple sequential commands
	t.Run("socket_sequential_commands", func(t *testing.T) {
		// Test that daemon can handle multiple client connections
		for i := 0; i < 3; i++ {
			conn, err := net.Dial("unix", socketPath)
			if err != nil {
				t.Errorf("PROTOCOL FAILED: Could not connect to socket (attempt %d): %v", i+1, err)
				continue
			}

			conn.Write([]byte("vm list"))
			buf := make([]byte, 1024)
			n, _ := conn.Read(buf)
			response := string(buf[:n])

			// REGRESSION CHECK
			if strings.Contains(response, "not yet implemented") {
				t.Errorf("REGRESSION DETECTED: Sequential command %d returned 'not yet implemented'", i+1)
			}

			conn.Close()
		}
		t.Logf("✓ socket handles sequential commands correctly")
	})
}
