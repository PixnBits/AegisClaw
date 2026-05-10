package main

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestUserJourney03CollaborativeTaskExecution(t *testing.T) {
	// Start hub
	hubCmd := exec.Command("/home/pixnbits/AegisClaw_lessons-learned/bin/aegishub", "start")
	hubCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test.sock")
	err := hubCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start hub: %v", err)
	}
	defer hubCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	// Start memory
	memCmd := exec.Command("/home/pixnbits/AegisClaw_lessons-learned/bin/memory")
	memCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test.sock")
	err = memCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start memory: %v", err)
	}
	defer memCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	// Start agent
	agentCmd := exec.Command("/home/pixnbits/AegisClaw_lessons-learned/bin/agent")
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
	// Similar setup
	hubCmd := exec.Command("/home/pixnbits/AegisClaw_lessons-learned/bin/aegishub", "start")
	hubCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test4.sock")
	err := hubCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start hub: %v", err)
	}
	defer hubCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	storeCmd := exec.Command("/home/pixnbits/AegisClaw_lessons-learned/bin/store")
	storeCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test4.sock")
	err = storeCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start store: %v", err)
	}
	defer storeCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	// builderCmd := exec.Command("./bin/builder")

	scribeCmd := exec.Command("/home/pixnbits/AegisClaw_lessons-learned/bin/court-scribe")
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
		cmd := exec.Command("/home/pixnbits/AegisClaw_lessons-learned/bin/court-persona", "--persona", p)
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
	// Start hub, store, builder, network-boundary for skill deployment
	hubCmd := exec.Command("/home/pixnbits/AegisClaw_lessons-learned/bin/aegishub", "start")
	hubCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test9.sock")
	err := hubCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start hub: %v", err)
	}
	defer hubCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	storeCmd := exec.Command("/home/pixnbits/AegisClaw_lessons-learned/bin/store")
	storeCmd.Env = append(os.Environ(), "AEGIS_HUB_SOCKET=/tmp/hub_test9.sock")
	err = storeCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start store: %v", err)
	}
	defer storeCmd.Process.Kill()
	time.Sleep(100 * time.Millisecond)

	// builderCmd := exec.Command("./bin/builder")

	netCmd := exec.Command("/home/pixnbits/AegisClaw_lessons-learned/bin/network-boundary")
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
