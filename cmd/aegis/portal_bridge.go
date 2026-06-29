package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"AegisClaw/internal/collab"
	"AegisClaw/internal/portalbridge"
	"AegisClaw/internal/sandbox"
	"AegisClaw/internal/transport/hubclient"
	"AegisClaw/internal/workspace"

	"github.com/mdlayher/vsock"
	"github.com/sirupsen/logrus"
)

// portalBridgeRPCTimeout bounds Store (and other Hub) work initiated from the web-portal
// guest bridge so a stuck Store RPC cannot wedge the serial portal-bridge connection.
const portalBridgeRPCTimeout = 25 * time.Second

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
	startDaemonPortalHubReceiver()
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
		ctx, cancel := context.WithTimeout(context.Background(), portalBridgeRPCTimeout)
		defer cancel()
		return sendToComponentViaHubContext(ctx, "store", command, payload)
	case "agent":
		return handlePortalChatAction(command, payload)
	case "daemon-orchestrator":
		return handlePortalOrchestratorAction(command, payload)
	default:
		return handlePortalDaemonLocal(command, payload)
	}
}

func handlePortalOrchestratorAction(command string, payload interface{}) (interface{}, error) {
	switch command {
	case "channel.fanout":
		return portalChannelFanout(payload)
	default:
		_, err := sendToComponentViaHub("daemon-orchestrator", command, payload)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"ok": true}, nil
	}
}

func portalChannelFanout(payload interface{}) (interface{}, error) {
	m, ok := payload.(map[string]interface{})
	if !ok {
		return map[string]interface{}{"status": "skipped", "reason": "invalid payload"}, nil
	}
	chID, _ := m["channel_id"].(string)
	from, _ := m["from"].(string)
	content := collab.PayloadContentString(m["content"])
	if chID == "" || content == "" {
		return map[string]interface{}{"status": "skipped", "reason": "missing channel or content"}, nil
	}
	if !collab.IsHumanPoster(from) {
		return map[string]interface{}{"status": "skipped", "reason": "not a human poster"}, nil
	}
	// Store channel.post already notifies channel-facilitator for agent turns.
	return map[string]interface{}{"status": "turn_scheduled", "channel_id": chID}, nil
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
	if orchestrator == nil || sessionID == "" || !collab.IsChatAgentSession(sessionID) {
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
	case "goal.submit":
		return portalGoalSubmit(payload)
	case "harness.get":
		return portalHarnessGet(payload)
	case "security.posture":
		return collectSecurityPostureForPortal(), nil
	case "event.approvals.list":
		return []interface{}{}, nil
	case "event.timers.list", "event.signals.list":
		return []interface{}{}, nil
	case "memory.list", "memory.search":
		return []interface{}{}, nil
	case "sessions.list":
		// Delegate to Store when daemon-local path is hit (should not happen if routing matches).
		return sendToComponentViaHub("store", command, payload)

	// Per-agent SETTINGS + SOUL (Phase 2). Atomic + validated writes. Routed to daemon for host FS safety.
	case "agent.settings.get", "agent.soul.get":
		m, _ := payload.(map[string]interface{})
		name, _ := m["name"].(string)
		if name == "" {
			name, _ = m["agent_id"].(string)
		}
		if name == "" {
			name = "default"
		}
		ws, err := workspace.LoadForAgent("", name)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(command, ".settings.get") {
			return map[string]interface{}{"agent": name, "settings": ws.SETTINGS}, nil
		}
		return map[string]interface{}{"agent": name, "soul": ws.SOUL}, nil
	case "agent.settings.set", "agent.soul.set":
		m, _ := payload.(map[string]interface{})
		name, _ := m["name"].(string)
		if name == "" {
			name, _ = m["agent_id"].(string)
		}
		if name == "" {
			name = "default"
		}
		if strings.HasSuffix(command, ".settings.set") {
			setIface := m["settings"]
			var set map[string]interface{}
			if mm, ok := setIface.(map[string]interface{}); ok {
				set = mm
			} else if b, ok := setIface.([]byte); ok {
				_ = json.Unmarshal(b, &set)
			}
			if err := workspace.WriteSettingsAtomic("", name, set); err != nil {
				return nil, err
			}
			return map[string]interface{}{"ok": true, "agent": name}, nil
		}
		content, _ := m["content"].(string)
		if err := workspace.WriteSoulAtomic("", name, content); err != nil {
			return nil, err
		}
		return map[string]interface{}{"ok": true, "agent": name}, nil
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
		if portalInfraVM(vm.ID, vm.Type) {
			continue
		}
		if vm.Status != sandbox.StatusRunning && vm.Status != "" {
			continue
		}
		role := portalVMRoleLabel(vm.ID, vm.Type)
		out = append(out, map[string]interface{}{
			"id":         vm.ID,
			"name":       vm.ID,
			"status":     string(vm.Status),
			"role":       role,
			"task":       role,
			"progress":   "—",
			"channel":    vm.Channel,
			"channel_id": vm.Channel,
		})
	}
	mergeChannelRosterIntoWorkers(&out, "main")
	return out
}

// mergeChannelRosterIntoWorkers adds on-demand channel members (e.g. project-manager-main)
// that are roster participants but not currently running as VMs. Permissions and trace
// APIs use these canonical ids even when the VM is cold.
func mergeChannelRosterIntoWorkers(out *[]interface{}, channelID string) {
	if out == nil || channelID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	chData, err := sendToComponentViaHubContext(ctx, "store", "channel.get", map[string]interface{}{"channel_id": channelID})
	if err != nil {
		return
	}
	ch, ok := chData.(map[string]interface{})
	if !ok {
		return
	}
	members, ok := ch["members"].([]interface{})
	if !ok {
		return
	}
	mergeChannelRosterFromMembers(out, channelID, members)
}

func mergeChannelRosterFromMembers(out *[]interface{}, channelID string, members []interface{}) {
	if out == nil || channelID == "" || len(members) == 0 {
		return
	}
	existing := make(map[string]struct{}, len(*out))
	for _, raw := range *out {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if id, _ := m["id"].(string); id != "" {
			existing[id] = struct{}{}
		}
		if name, _ := m["name"].(string); name != "" {
			existing[name] = struct{}{}
		}
	}
	for _, raw := range members {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		role = collab.NormalizeMemberRole(role)
		if role == "" {
			continue
		}
		agentID := collab.ChannelMemberAgentID(role, channelID)
		if agentID == "" {
			continue
		}
		if _, seen := existing[agentID]; seen {
			continue
		}
		task := portalVMRoleLabel(agentID, role)
		*out = append(*out, map[string]interface{}{
			"id":         agentID,
			"name":       agentID,
			"status":     "standby",
			"role":       task,
			"task":       task,
			"progress":   "—",
			"channel":    channelID,
			"channel_id": channelID,
		})
		existing[agentID] = struct{}{}
	}
}

func portalInfraVM(id, vmType string) bool {
	switch id {
	case "hub", "store", "network-boundary", "web-portal", "aegishub", "court-scribe":
		return true
	}
	if strings.HasPrefix(id, "memory-") {
		return true
	}
	switch vmType {
	case "hub", "store", "network-boundary", "web-portal", "memory", "court-scribe":
		return true
	}
	return false
}

func portalVMRoleLabel(id, vmType string) string {
	if strings.HasPrefix(id, "project-manager") || vmType == "project-manager" {
		return "project-manager"
	}
	if strings.HasPrefix(id, "court-persona-") || vmType == "court-persona" {
		return "court"
	}
	if vmType != "" && vmType != "agent" {
		return vmType
	}
	return "agent"
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
	stats := readHostSystemStats()
	hostRAMUsedMB := int64(toFloat64(stats["host_ram_used_mb"]))
	hostRAMTotalMB := int64(toFloat64(stats["host_ram_total_mb"]))
	hostLoadAvg1 := toFloat64(stats["host_load_avg_1"])
	stats["host_ram_label"] = fmtHostRAMLabel(hostRAMUsedMB, hostRAMTotalMB)
	stats["host_load_label"] = fmt.Sprintf("%.2f", hostLoadAvg1)
	return stats
}

// startDaemonPortalHubReceiver registers "daemon" on AegisHub so the web-portal guest
// (inverted hub bridge :9101) can reach presentation aggregation actions (worker.list,
// sandbox.list, system.stats) without host-intercepted HTTP. See web-portal.md + host-daemon.md.
func startDaemonPortalHubReceiver() {
	go func() {
		hubPath := hubSocketPath()
		for {
			pub, priv, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				time.Sleep(time.Second)
				continue
			}
			client, err := hubclient.DialUnix(hubPath, priv)
			if err != nil {
				time.Sleep(time.Second)
				continue
			}
			if _, err := client.Register(context.Background(), "daemon", pub, "phase1"); err != nil {
				client.Close()
				time.Sleep(time.Second)
				continue
			}
			logrus.Info("daemon portal hub receiver registered for web-portal presentation actions")
			for {
				msg, err := client.Receive(context.Background())
				if err != nil {
					break
				}
				if msg.Destination != "daemon" {
					continue
				}
				payload, actionErr := handlePortalBridgeAction(msg.Command, msg.Payload)
				resp := hubclient.Message{
					Source:      "daemon",
					Destination: msg.Source,
					Command:     msg.Command,
					Timestamp:   time.Now().UTC().Format(time.RFC3339),
				}
				if actionErr != nil {
					resp.Command = "error"
					resp.Payload = actionErr.Error()
				} else {
					resp.Payload = payload
				}
				_ = client.Reply(context.Background(), resp)
			}
			client.Close()
			time.Sleep(time.Second)
		}
	}()
}
