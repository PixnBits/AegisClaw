package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"AegisClaw/internal/portalbridge"
	"AegisClaw/internal/sandbox"
	"AegisClaw/internal/transport/hubclient"

	"github.com/mdlayher/vsock"
	"github.com/sirupsen/logrus"
)

// portalBridgeMsg matches the web-portal hub bridge wire format.
type portalBridgeMsg struct {
	Source      string      `json:"source"`
	Destination string      `json:"destination"`
	Command     string      `json:"command"`
	Payload     interface{} `json:"payload"`
	Timestamp   string      `json:"timestamp"`
	Signature   string      `json:"signature"`
}

// startPortalBridge listens on vsock for the Web Portal microVM when it cannot
// reach AegisHub directly (web-portal-vm.md: host-mediated bridge on port 1030).
func startPortalBridge() {
	go func() {
		port := uint32(hubclient.PortalBridgeVsockPort)
		l, err := vsock.Listen(port, nil)
		if err != nil {
			logrus.Warnf("portal bridge: vsock listen on port %d failed (web-portal guest may need direct hub vsock): %v", port, err)
			return
		}
		logrus.Infof("portal bridge: listening on vsock port %d for web-portal microVM", port)
		for {
			conn, err := l.Accept()
			if err != nil {
				logrus.Warnf("portal bridge accept: %v", err)
				continue
			}
			go handlePortalBridgeConn(conn)
		}
	}()
}

func handlePortalBridgeConn(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	for {
		var msg portalBridgeMsg
		if err := dec.Decode(&msg); err != nil {
			return
		}
		resp := dispatchPortalBridge(msg)
		_ = enc.Encode(resp)
	}
}

func dispatchPortalBridge(msg portalBridgeMsg) portalBridgeMsg {
	resp := portalBridgeMsg{
		Source:      "daemon",
		Destination: msg.Source,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	payload, err := handlePortalBridgeAction(msg.Command, msg.Payload)
	if err != nil {
		resp.Command = "error"
		resp.Payload = err.Error()
		return resp
	}
	resp.Command = msg.Command
	resp.Payload = payload
	return resp
}

func handlePortalBridgeAction(command string, payload interface{}) (interface{}, error) {
	dest := portalbridge.Destination(command)
	switch dest {
	case "store":
		return sendToComponentViaHub("store", command, payload)
	case "agent":
		return handlePortalChatAction(command, payload)
	default:
		return handlePortalDaemonLocal(command, payload)
	}
}

func handlePortalChatAction(command string, payload interface{}) (interface{}, error) {
	sessionID := ""
	if m, ok := payload.(map[string]interface{}); ok {
		sessionID, _ = m["session_id"].(string)
	}

	// SSE poll commands — forward to agent VM(s), never block chat.message.
	switch command {
	case "chat.tool_events", "chat.thought_events", "chat.stream_progress":
		ensurePairedAgentForSession(sessionID)
		targets := portalChatPollTargets(sessionID)
		var lastErr error
		for _, target := range targets {
			resp, err := sendToComponentViaHubRetry(target, command, payload, 8*time.Second)
			if err == nil && resp != nil {
				return normalizeChatHubResponse(resp), nil
			}
			if err != nil {
				lastErr = err
			}
		}
		if command == "chat.thought_events" {
			if lastErr != nil {
				return []interface{}{}, nil
			}
			return []interface{}{}, nil
		}
		if lastErr != nil {
			return map[string]interface{}{"content": "", "thinking": ""}, nil
		}
		return map[string]interface{}{"content": "", "thinking": ""}, nil
	}

	ensurePairedAgentForSession(sessionID)

	targets := portalChatAgentTargets(sessionID)

	commands := []string{command}
	if command == "chat.message" {
		commands = append(commands, "user.turn")
	}

	var lastErr error
	for _, cmd := range commands {
		hubPayload := payload
		if cmd == "user.turn" {
			hubPayload = chatPayloadForUserTurn(payload)
		}
		for _, target := range targets {
			resp, err := sendToComponentViaHubRetry(target, cmd, hubPayload, 90*time.Second)
			if err == nil && resp != nil {
				return normalizeChatHubResponse(resp), nil
			}
			if err != nil {
				lastErr = err
			} else {
				lastErr = fmt.Errorf("empty hub response from %s", target)
			}
		}
	}

	if lastErr != nil {
		logrus.Warnf("portal bridge chat.%s: %v (targets=%v)", command, lastErr, targets)
		return nil, fmt.Errorf("agent unavailable: %v", lastErr)
	}
	return nil, fmt.Errorf("agent unavailable: not registered on hub (targets=%v)", targets)
}

// portalChatPollTargets returns agent VM IDs for SSE poll commands. When session_id is
// set, only the paired agent is queried so empty responses from unrelated agents do
// not mask in-flight progress for the active session.
func portalChatPollTargets(sessionID string) []string {
	if sessionID != "" {
		return []string{"agent-" + sessionID}
	}
	return portalChatAgentTargets("")
}

func portalChatAgentTargets(sessionID string) []string {
	seen := make(map[string]bool)
	var targets []string
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		targets = append(targets, id)
	}
	if sessionID != "" {
		add("agent-" + sessionID)
	}
	if orchestrator != nil {
		vms, err := orchestrator.ListVMs(context.Background())
		if err == nil {
			for _, vm := range vms {
				if vm.Status != sandbox.StatusRunning && vm.Status != "" {
					continue
				}
				if vm.Type == "agent" || strings.HasPrefix(vm.ID, "agent-") {
					add(vm.ID)
				}
			}
		}
	}
	add("agent")
	return targets
}

// ensurePairedAgentForSession launches agent+memory for a chat session when missing,
// or re-attaches hub bridges when the pair is already running.
func ensurePairedAgentForSession(sessionID string) {
	if orchestrator == nil || sessionID == "" {
		return
	}
	ctx := context.Background()
	agentID := "agent-" + sessionID
	if st, err := orchestrator.GetVMStatus(ctx, agentID); err == nil && st == sandbox.StatusRunning {
		startGuestHubBridgesForSession(sessionID)
		return
	}
	// Start host->guest hub bridges *before* or concurrent with StartPaired so the
	// bridge retry loop overlaps guest boot time (reduces effective hub_dialed latency
	// for the agent guest, which was the ~1.3-1.8s pole in early measurements).
	// The bridge has its own retry until vsock ready.
	go startGuestHubBridgesForSession(sessionID)
	if _, _, err := orchestrator.StartPairedAgentAndMemory(ctx, sessionID); err != nil {
		logrus.Debugf("portal bridge: paired agent launch for %s: %v", sessionID, err)
		return
	}
	// Poll for the agent to be ready using the sentinel (written at register_complete in guest).
	// This makes the readiness tight using the sentinel, reducing the "agent unavailable" and fixed waits for <1s.
	_, _ = sendToComponentViaHubRetry("agent-"+sessionID, "component.ready", nil, 30*time.Second)

}

func chatPayloadForUserTurn(payload interface{}) interface{} {
	m, ok := payload.(map[string]interface{})
	if !ok {
		return payload
	}
	out := map[string]interface{}{}
	if input, ok := m["input"].(string); ok {
		out["input"] = input
	}
	if sessionID, ok := m["session_id"].(string); ok && sessionID != "" {
		out["session"] = sessionID
	}
	if hist, ok := m["history"]; ok {
		out["history"] = hist
	}
	return out
}

func normalizeChatHubResponse(resp interface{}) map[string]interface{} {
	if resp == nil {
		return map[string]interface{}{
			"content": "No response from the agent yet. Wait a few seconds after starting a session and try again.",
			"note":    "empty hub payload",
		}
	}
	switch v := resp.(type) {
	case string:
		if v == "" || v == "<nil>" {
			return map[string]interface{}{
				"content": "The agent returned an empty response. The paired agent VM may still be starting.",
				"note":    "empty agent text",
			}
		}
		return map[string]interface{}{"content": v}
	case map[string]interface{}:
		if _, ok := v["content"]; ok {
			if c, ok := v["content"].(string); ok && (c == "" || c == "<nil>") {
				v["content"] = "The agent returned an empty response. The paired agent VM may still be starting."
			}
			return v
		}
		if c, ok := v["response"].(string); ok {
			return map[string]interface{}{"content": c}
		}
		return map[string]interface{}{"content": fmt.Sprintf("%v", v)}
	default:
		s := fmt.Sprintf("%v", resp)
		if s == "" || s == "<nil>" {
			return map[string]interface{}{
				"content": "The agent returned an empty response. The paired agent VM may still be starting.",
				"note":    "empty agent payload",
			}
		}
		return map[string]interface{}{"content": s}
	}
}

func handlePortalDaemonLocal(command string, payload interface{}) (interface{}, error) {
	switch command {
	case "worker.list":
		return portalWorkerList(), nil
	case "sandbox.list":
		return portalSandboxList(), nil
	case "system.stats":
		return portalSystemStats(), nil
	case "chat.tool_events", "chat.thought_events":
		return []interface{}{}, nil
	case "chat.stream_progress":
		if resp, err := sendToComponentViaHub("agent", command, payload); err == nil {
			if m, ok := resp.(map[string]interface{}); ok {
				return m, nil
			}
		}
		if m, ok := payload.(map[string]interface{}); ok {
			return map[string]interface{}{
				"stream_id": m["stream_id"],
				"content":   "",
				"thinking":  "",
			}, nil
		}
		return map[string]interface{}{"content": "", "thinking": ""}, nil
	case "event.approvals.list":
		return []interface{}{}, nil
	case "event.timers.list", "event.signals.list":
		return []interface{}{}, nil
	case "memory.list", "memory.search":
		return []interface{}{}, nil
	case "sessions.list":
		// Delegate to Store when daemon-local path is hit (should not happen if routing matches).
		return sendToComponentViaHub("store", command, payload)
	default:
		if strings.HasPrefix(command, "sessions.") || strings.HasPrefix(command, "team.") {
			return sendToComponentViaHub("store", command, payload)
		}
		return map[string]interface{}{}, nil
	}
}

func portalWorkerList() []interface{} {
	if orchestrator == nil {
		return []interface{}{}
	}
	vms, err := orchestrator.ListVMs(context.Background())
	if err != nil {
		return []interface{}{}
	}
	out := make([]interface{}, 0)
	for _, vm := range vms {
		if vm.Type == "agent" || strings.HasPrefix(vm.ID, "agent") {
			out = append(out, map[string]interface{}{
				"id":     vm.ID,
				"name":   vm.ID,
				"status": string(vm.Status),
				"role":   "agent",
			})
		}
	}
	return out
}

func portalSandboxList() []interface{} {
	if orchestrator == nil {
		return []interface{}{}
	}
	vms, err := orchestrator.ListVMs(context.Background())
	if err != nil {
		return []interface{}{}
	}
	out := make([]interface{}, 0, len(vms))
	for _, vm := range vms {
		out = append(out, map[string]interface{}{
			"id":        vm.ID,
			"name":      vm.ID,
			"status":    string(vm.Status),
			"type":      vm.Type,
			"vcpus":     float64(vm.Config.VCpus),
			"memory_mb": float64(vm.Config.Memory),
		})
	}
	return out
}

func portalSystemStats() map[string]interface{} {
	return readHostSystemStats()
}
