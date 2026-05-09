package main

import (
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