package main

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type Message struct {
	Source      string      `json:"source"`
	Destination string      `json:"destination"`
	Command     string      `json:"command"`
	Payload     interface{} `json:"payload"`
	Timestamp   string      `json:"timestamp"`
	Signature   string      `json:"signature"`
}

func TestUserJourney03CollaborativeTaskExecution(t *testing.T) {
	if os.Getenv("AEGIS_RUN_MULTI_PROCESS_JOURNEYS") == "" {
		t.Skip("set AEGIS_RUN_MULTI_PROCESS_JOURNEYS=1 to run multi-process journey tests")
	}

	rootDir := repoRoot(t)
	hubBinary := buildRepoBinary(t, rootDir, "./cmd/aegishub", "aegishub")
	memoryBinary := buildRepoBinary(t, rootDir, "./cmd/memory", "memory")
	agentBinary := buildRepoBinary(t, rootDir, "./cmd/agent", "agent")

	// Start hub
	hubCmd := exec.Command(hubBinary, "start")
	hubCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test.sock")
	err := hubCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start hub: %v", err)
	}
	defer hubCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	// Start memory
	memCmd := exec.Command(memoryBinary)
	memCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test.sock")
	err = memCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start memory: %v", err)
	}
	defer memCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	// Start agent
	agentCmd := exec.Command(agentBinary)
	agentCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test.sock")
	err = agentCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start agent: %v", err)
	}
	defer agentCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	// Connect and send collaborative task message
	conn, err := net.Dial("unix", "/tmp/hub_test.sock")
	if err != nil {
		t.Fatalf("Failed to connect to hub: %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// Register client
	regMsg := Message{
		Source:      "client",
		Destination: "hub",
		Command:     "register",
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "dummy",
	}
	err = encoder.Encode(regMsg)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}
	// Consume response
	var resp map[string]interface{}
	err = decoder.Decode(&resp)
	if err != nil {
		t.Fatalf("Failed to decode register response: %v", err)
	}

	// Send task message
	taskMsg := Message{
		Source:      "client",
		Destination: "agent1",
		Command:     "collaborative_task",
		Payload:     "Execute a collaborative task",
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "dummy",
	}
	err = encoder.Encode(taskMsg)
	if err != nil {
		t.Fatalf("Failed to send task: %v", err)
	}

	// Receive response
	var received Message
	err = decoder.Decode(&received)
	if err != nil {
		t.Fatalf("Failed to receive response: %v", err)
	}

	if received.Command != "response" {
		t.Errorf("Expected response, got %s", received.Command)
	}
}

func TestUserJourney04CreatingIteratingNewSkill(t *testing.T) {
	if os.Getenv("AEGIS_RUN_MULTI_PROCESS_JOURNEYS") == "" {
		t.Skip("set AEGIS_RUN_MULTI_PROCESS_JOURNEYS=1 to run multi-process journey tests")
	}

	rootDir := repoRoot(t)
	hubBinary := buildRepoBinary(t, rootDir, "./cmd/aegishub", "aegishub")
	storeBinary := buildRepoBinary(t, rootDir, "./cmd/store", "store")
	scribeBinary := buildRepoBinary(t, rootDir, "./cmd/court-scribe", "court-scribe")
	personaBinary := buildRepoBinary(t, rootDir, "./cmd/court-persona", "court-persona")

	// Similar setup
	hubCmd := exec.Command(hubBinary, "start")
	hubCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test4.sock")
	err := hubCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start hub: %v", err)
	}
	defer hubCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	storeCmd := exec.Command(storeBinary)
	storeCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test4.sock")
	err = storeCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start store: %v", err)
	}
	defer storeCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	// builderCmd := exec.Command("./bin/builder")

	scribeCmd := exec.Command(scribeBinary)
	scribeCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test4.sock")
	err = scribeCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start scribe: %v", err)
	}
	defer scribeCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	// Start personas
	personas := []string{"ciso", "security_architect", "architect", "senior_coder", "tester", "efficiency", "user_advocate"}
	var personaCmds []*exec.Cmd
	for _, p := range personas {
		cmd := exec.Command(personaBinary, "--persona", p)
		cmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test4.sock")
		err = cmd.Start()
		if err != nil {
			t.Fatalf("Failed to start persona %s: %v", p, err)
		}
		personaCmds = append(personaCmds, cmd)
	}
	defer func() {
		for _, cmd := range personaCmds {
			cmd.Process.Kill()
		}
	}()
	time.Sleep(200 * time.Millisecond)

	// Connect client
	conn, err := net.Dial("unix", "/tmp/hub_test4.sock")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// Register
	regMsg := Message{
		Source:      "client",
		Destination: "hub",
		Command:     "register",
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "dummy",
	}
	encoder.Encode(regMsg)
	// Consume response
	var resp map[string]interface{}
	decoder.Decode(&resp)

	// Create proposal
	propMsg := Message{
		Source:      "client",
		Destination: "store",
		Command:     "proposal.create",
		Payload:     map[string]interface{}{"id": "skill123", "description": "New skill"},
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "dummy",
	}
	encoder.Encode(propMsg)
	var propResp Message
	decoder.Decode(&propResp)

	if propResp.Command != "proposal.created" {
		t.Errorf("Expected proposal.created, got %s", propResp.Command)
	}

	// Simulate court review (since stubs approve)
	time.Sleep(500 * time.Millisecond) // Allow time for votes
}

func TestUserJourney09AddingDiscordMonitorSkill(t *testing.T) {
	if os.Getenv("AEGIS_RUN_MULTI_PROCESS_JOURNEYS") == "" {
		t.Skip("set AEGIS_RUN_MULTI_PROCESS_JOURNEYS=1 to run multi-process journey tests")
	}

	rootDir := repoRoot(t)
	hubBinary := buildRepoBinary(t, rootDir, "./cmd/aegishub", "aegishub")
	storeBinary := buildRepoBinary(t, rootDir, "./cmd/store", "store")
	networkBoundaryBinary := buildRepoBinary(t, rootDir, "./cmd/network-boundary", "network-boundary")

	// Start hub, store, builder, network-boundary for skill deployment
	hubCmd := exec.Command(hubBinary, "start")
	hubCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test9.sock")
	err := hubCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start hub: %v", err)
	}
	defer hubCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	storeCmd := exec.Command(storeBinary)
	storeCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test9.sock")
	err = storeCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start store: %v", err)
	}
	defer storeCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	// builderCmd := exec.Command("./bin/builder")

	netCmd := exec.Command(networkBoundaryBinary)
	netCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test9.sock")
	err = netCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start network-boundary: %v", err)
	}
	defer netCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	// Connect client
	conn, err := net.Dial("unix", "/tmp/hub_test9.sock")
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// Register
	regMsg := Message{
		Source:      "client",
		Destination: "hub",
		Command:     "register",
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "dummy",
	}
	encoder.Encode(regMsg)
	// Consume response
	var resp map[string]interface{}
	decoder.Decode(&resp)

	// Register skill
	skillMsg := Message{
		Source:      "client",
		Destination: "store",
		Command:     "skill.register",
		Payload:     map[string]interface{}{"id": "discord-monitor", "description": "Monitor Discord"},
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "dummy",
	}
	encoder.Encode(skillMsg)
	var skillResp Message
	decoder.Decode(&skillResp)

	if skillResp.Command != "skill.registered" {
		t.Errorf("Expected skill.registered, got %s", skillResp.Command)
	}

	// Test tool list
	toolMsg := Message{
		Source:      "client",
		Destination: "hub",
		Command:     "tool.list",
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "dummy",
	}
	encoder.Encode(toolMsg)
	var toolResp Message
	decoder.Decode(&toolResp)

	if toolResp.Command != "tool.list" {
		t.Errorf("Expected tool.list, got %s", toolResp.Command)
	}

	// Test daemon status for VM states
	testDaemonStatus(t)
}

func testDaemonStatus(t *testing.T) {
	home, _ := os.UserHomeDir()
	socket := home + "/.aegis/daemon.sock"
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatalf("Failed to connect to daemon: %v", err)
	}
	defer conn.Close()

	conn.Write([]byte("status"))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		t.Fatalf("Expected status response")
	}
	status := string(buf[:n])
	if !strings.Contains(status, "Memory VM: running") {
		t.Errorf("Expected Memory VM running, got %s", status)
	}
	if !strings.Contains(status, "Store VM: running") {
		t.Errorf("Expected Store VM running, got %s", status)
	}
	if !strings.Contains(status, "Running VMs: 8") {
		t.Errorf("Expected 8 running VMs, got %s", status)
	}
}

func TestUserJourney05MonitoringAgentActivity(t *testing.T) {
	// Placeholder: Test monitoring via logs and status
	t.Skip("Placeholder for journey 05")
}

func TestUserJourney06ReviewingCourtDecisions(t *testing.T) {
	// Placeholder: Test court decision retrieval
	t.Skip("Placeholder for journey 06")
}

func TestUserJourney07GrantingAdjustingAutonomy(t *testing.T) {
	// Placeholder: Test autonomy settings
	t.Skip("Placeholder for journey 07")
}

func TestUserJourney08MultiAgentTeamWorkflows(t *testing.T) {
	// Placeholder: Test multiple agents collaborating
	t.Skip("Placeholder for journey 08")
}
