package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

const testSocketPath = "/tmp/aegis_test.sock"

func TestDaemonStartAndStatus(t *testing.T) {
	// Clean up
	os.Remove(testSocketPath)

	// Start daemon in background
	cmd := exec.Command("/home/pixnbits/AegisClaw_lessons-learned/aegis", "start")
	cmd.Env = append(os.Environ(), "AEGIS_SOCKET="+testSocketPath)
	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}
	defer cmd.Process.Kill()

	// Wait for socket to be created
	time.Sleep(100 * time.Millisecond)

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

	expected := "running\n"
	if string(buf[:n]) != expected {
		t.Errorf("Expected %q, got %q", expected, string(buf[:n]))
	}

	// Test start-vm
	conn.Write([]byte("start-vm test busybox\n"))
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read start-vm response: %v", err)
	}

	expected = "started\n"
	if string(buf[:n]) != expected {
		t.Errorf("Expected %q, got %q", expected, string(buf[:n]))
	}
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
		t.Error("Expected running with own PID")
	}
}


