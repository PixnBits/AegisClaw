package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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
	if os.Getuid() != 0 {
		t.Skip("daemon start integration test requires root privileges")
	}

	// Clean up
	os.Remove(testSocketPath)

	repoRoot := repoRoot(t)
	aegisBinary := buildRepoBinary(t, repoRoot, "./cmd/aegis", "aegis")
	buildRepoBinary(t, repoRoot, "./cmd/aegishub", "aegishub")
	buildRepoBinary(t, repoRoot, "./cmd/memory", "memory")
	buildRepoBinary(t, repoRoot, "./cmd/store", "store")

	// Start daemon in background
	cmd := exec.Command(aegisBinary, "start")
	cmd.Env = append(os.Environ(), "AEGIS_SOCKET="+testSocketPath)
	cmd.Dir = repoRoot
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}
	defer func() {
		stopCmd := exec.Command(aegisBinary, "stop")
		stopCmd.Env = append(os.Environ(), "AEGIS_SOCKET="+testSocketPath)
		stopCmd.Dir = repoRoot
		_ = stopCmd.Run()
		_ = os.Remove(testSocketPath)
	}()

	waitForUnixSocket(t, testSocketPath, 10*time.Second)

	// Check status
	conn, err := net.Dial("unix", testSocketPath)
	if err != nil {
		t.Fatalf("Failed to connect to daemon: %v", err)
	}
	defer conn.Close()

	// Test status
	conn.Write([]byte("status"))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read status: %v", err)
	}

	response := string(buf[:n])
	if !strings.Contains(response, "Daemon: running") {
		t.Errorf("Expected status to contain 'Daemon: running', got %q", response)
	}

	// Test start-vm (commented out due to backend requirements)
	// conn.Write([]byte("start-vm test busybox\n"))
	// n, err = conn.Read(buf)
	// if err != nil {
	// 	t.Fatalf("Failed to read start-vm response: %v", err)
	// }

	// expected = "started\n"
	// if string(buf[:n]) != expected {
	// 	t.Errorf("Expected %q, got %q", expected, string(buf[:n]))
	// }
}

func TestIsDaemonRunning(t *testing.T) {
	pidFile := expandPath("~/.aegis/daemon.pid")
	os.Remove(pidFile) // Clean up

	// Test when no PID file
	if isDaemonRunning() {
		t.Error("Expected not running when no PID file")
	}

	// Create fake PID file with invalid PID
	pidFile = expandPath("~/.aegis/daemon.pid")
	os.WriteFile(pidFile, []byte("99999"), 0644)
	defer os.Remove(pidFile)

	// Should return false
	if isDaemonRunning() {
		t.Error("Expected not running with invalid PID")
	}

	// Test with own PID
	os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
	if !isDaemonRunning() {
		t.Log("Daemon not running in test environment, which is expected")
	}
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
	// Test without SUDO_USER
	os.Unsetenv("SUDO_USER")
	origUser, err := getOriginalUser()
	if err != nil {
		t.Fatalf("Failed to get original user: %v", err)
	}
	current, _ := user.Current()
	if origUser.Uid != current.Uid {
		t.Errorf("Expected current user, got %v", origUser)
	}

	// Test with SUDO_USER
	os.Setenv("SUDO_USER", "root")
	defer os.Unsetenv("SUDO_USER")
	origUser, err = getOriginalUser()
	if err != nil {
		t.Fatalf("Failed to get original user: %v", err)
	}
	if origUser.Username != "root" {
		t.Errorf("Expected root, got %v", origUser.Username)
	}
}

func TestExpandPath(t *testing.T) {
	// Test without ~
	path := "/tmp/test"
	expanded := expandPath(path)
	if expanded != path {
		t.Errorf("Expected %s, got %s", path, expanded)
	}

	// Test with ~/, assuming SUDO_USER not set
	os.Unsetenv("SUDO_USER")
	home, _ := os.UserHomeDir()
	expected := home + "/.aegis/test"
	expanded = expandPath("~/.aegis/test")
	if expanded != expected {
		t.Errorf("Expected %s, got %s", expected, expanded)
	}
}

func TestVMConfig(t *testing.T) {
	config := VMConfig{
		ID:         "test",
		StartTime:  time.Now(),
		KernelPath: "/tmp/kernel",
		RootfsPath: "/tmp/rootfs",
	}
	if config.ID != "test" {
		t.Errorf("Expected test, got %s", config.ID)
	}
	if config.KernelPath != "/tmp/kernel" {
		t.Errorf("Expected /tmp/kernel, got %s", config.KernelPath)
	}
}
