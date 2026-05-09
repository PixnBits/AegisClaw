package main

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

const testHubSocketPath = "/tmp/aegishub_test.sock"

func TestHubRoundTrip(t *testing.T) {
	// Clean up
	os.Remove(testHubSocketPath)

	// Start hub in background
	cmd := exec.Command("/home/pixnbits/AegisClaw_lessons-learned/aegishub", "start")
	cmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET="+testHubSocketPath)
	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start hub: %v", err)
	}
	defer cmd.Process.Kill()

	// Wait for socket
	time.Sleep(100 * time.Millisecond)

	// Connect client1
	conn1, err := net.Dial("unix", testHubSocketPath)
	if err != nil {
		t.Fatalf("Failed to connect client1: %v", err)
	}
	defer conn1.Close()

	// Connect client2
	conn2, err := net.Dial("unix", testHubSocketPath)
	if err != nil {
		t.Fatalf("Failed to connect client2: %v", err)
	}
	defer conn2.Close()

	// Register client2
	encoder2 := json.NewEncoder(conn2)
	decoder2 := json.NewDecoder(conn2)
	regMsg := Message{
		Source:      "client2",
		Destination: "hub",
		Command:     "register",
		Payload:     nil,
		Timestamp:   "2026-05-09T19:20:00Z",
		Signature:   "dummy",
	}
	err = encoder2.Encode(regMsg)
	if err != nil {
		t.Fatalf("Failed to register client2: %v", err)
	}
	// Consume response
	var resp map[string]interface{}
	err = decoder2.Decode(&resp)
	if err != nil {
		t.Fatalf("Failed to decode register response: %v", err)
	}

	// Send message from client1 to client2
	msg := Message{
		Source:      "client1",
		Destination: "client2",
		Command:     "test",
		Payload:     "hello",
		Timestamp:   "2026-05-09T19:20:00Z",
		Signature:   "dummy",
	}
	encoder1 := json.NewEncoder(conn1)
	err = encoder1.Encode(msg)
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	// Client2 should receive the message
	var received Message
	err = decoder2.Decode(&received)
	if err != nil {
		t.Fatalf("Failed to receive message: %v", err)
	}

	if received.Source != "client1" || received.Destination != "client2" || received.Command != "test" {
		t.Errorf("Received wrong message: %+v", received)
	}
}