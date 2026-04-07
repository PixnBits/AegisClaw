package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/audit"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/tui"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const reactMaxIterations = 10

// agentVMRequest is the JSON structure sent to the agent VM for chat.message.
// It mirrors the guest-agent's Request{Type, Payload} envelope.
type agentVMRequest struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// agentVMResponse is the envelope returned by the guest-agent.
type agentVMResponse struct {
	ID      string          `json:"id"`
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// agentChatPayload is placed in agentVMRequest.Payload for chat.message requests.
// It mirrors the guest-agent's ChatMessagePayload.
type agentChatPayload struct {
	Messages []agentChatMsg `json:"messages"`
	Model    string         `json:"model"`
	StreamID string         `json:"stream_id,omitempty"`
	// StructuredOutput requests JSON-mode enforcement in the guest-agent (Phase 0).
	// When true the guest-agent uses Ollama format=json and validates the response
	// schema before returning.
	StructuredOutput bool `json:"structured_output,omitempty"`
}

// agentChatMsg is a single message in the conversation sent to the agent VM.
type agentChatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// agentChatResponse is the ChatResponse returned inside agentVMResponse.Data.
// Mirrors the guest-agent's ChatResponse type.
type agentChatResponse struct {
	Status   string `json:"status"`            // "final" | "tool_call"
	Role     string `json:"role,omitempty"`    // present when status=="final"
	Content  string `json:"content,omitempty"` // present when status=="final"
	Thinking string `json:"thinking,omitempty"`
	Tool     string `json:"tool,omitempty"` // present when status=="tool_call"
	Args     string `json:"args,omitempty"` // present when status=="tool_call"
}

// makeChatMessageHandler returns an API handler that drives the full ReAct loop
// by forwarding conversation turns to the agent microVM (D2-b).
//
// The loop:
//  1. ensureAgentVM — starts the agent VM on first call (DC).
//  2. Build messages (system prompt + history + user input + any accumulated tool results).
//  3. Send to agent VM → parse agentChatResponse.
//  4. If status=="tool_call": execute via toolRegistry, append result, go to 2.
//  5. If status=="final": return the assistant content to the CLI.
//  6. If max iterations reached: return a polite error.
func makeChatMessageHandler(env *runtimeEnv, toolRegistry *ToolRegistry) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.ChatMessageRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Input == "" {
			return &api.Response{Error: "input is required"}
		}
		if env.ToolEvents == nil {
			env.ToolEvents = NewToolEventBuffer(400)
		}
		if env.ThoughtEvents == nil {
			env.ThoughtEvents = NewThoughtEventBuffer(600)
		}

		agentVMID, err := ensureAgentVM(ctx, env)
		if err != nil {
			return &api.Response{Error: "agent VM unavailable: " + err.Error()}
		}

		// Session tracking (Phase 1 – session routing tools).
		// Determine the session ID for this request.  Prefer req.SessionID,
		// fall back to StreamID, then generate a stable one for the lifetime
		// of this VM.
		sessionID := strings.TrimSpace(req.SessionID)
		if sessionID == "" {
			sessionID = strings.TrimSpace(req.StreamID)
		}
		if sessionID == "" {
			// Use the agent VM ID as a stable per-VM session key.
			sessionID = agentVMID
		}
		if env.Sessions != nil {
			env.Sessions.Open(sessionID, agentVMID)
			env.Sessions.SetStatus(sessionID, "active")
			env.Sessions.AppendMessage(sessionID, agentVMID, "user", req.Input)
		}

		model := env.Config.Ollama.DefaultModel
		systemPrompt := buildDaemonSystemPrompt(env)
		systemPrompt += buildMemoryStatusGuard(env)

		// Phase 1: Auto-inject a compact memory summary at the top of every
		// new conversation turn so the agent has immediate context from prior
		// sessions.  Only retrieve on the first turn (empty history) to avoid
		// ballooning the context on subsequent turns.
		if len(req.History) == 0 && env.MemoryStore != nil {
			if summary := buildMemorySummary(env, req.Input); summary != "" {
				systemPrompt += "\n\nRELEVANT MEMORY CONTEXT (from prior sessions):\n" + summary + "\n"
			}
		}

		// Phase 2: Inject pending async items summary on the first turn.
		if len(req.History) == 0 && env.EventBus != nil {
			if pending := env.EventBus.PendingSummary(); pending != "" {
				systemPrompt += "\n\nPENDING ASYNC ITEMS: " + pending + "\nUse `list_pending_async` to see details.\n"
			}
		}

		// Seed conversation with system prompt + prior history + new user turn.
		msgs := make([]agentChatMsg, 0, len(req.History)+2)
		msgs = append(msgs, agentChatMsg{Role: "system", Content: systemPrompt})
		for _, h := range req.History {
			if h.Role == "system" {
				continue
			}
			msgs = append(msgs, agentChatMsg{Role: h.Role, Content: h.Content})
		}
		// Only append req.Input if it's not already the last message in
		// history. The TUI appends the user message to the chat log before
		// calling SendMessage (so it renders immediately), which means the
		// history already contains the current input.
		alreadyInHistory := false
		if n := len(msgs); n > 0 && msgs[n-1].Role == "user" && msgs[n-1].Content == req.Input {
			alreadyInHistory = true
		}
		if !alreadyInHistory {
			msgs = append(msgs, agentChatMsg{Role: "user", Content: req.Input})
		}

		memoryToolEvidence := false
		toolTrace := make([]map[string]interface{}, 0)
		thinkingTrace := make([]map[string]interface{}, 0)

		for i := 0; i < reactMaxIterations; i++ {
			payloadBytes, _ := json.Marshal(agentChatPayload{
				Messages:         msgs,
				Model:            model,
				StreamID:         req.StreamID,
				StructuredOutput: env.Config.Agent.StructuredOutput,
			})
			vmReq := agentVMRequest{
				ID:      uuid.New().String(),
				Type:    "chat.message",
				Payload: json.RawMessage(payloadBytes),
			}

			raw, err := env.Runtime.SendToVM(ctx, agentVMID, vmReq)
			if err != nil {
				return &api.Response{Error: "agent VM error: " + err.Error()}
			}

			var vmResp agentVMResponse
			if err := json.Unmarshal(raw, &vmResp); err != nil {
				return &api.Response{Error: "malformed agent response: " + err.Error()}
			}
			if !vmResp.Success {
				return &api.Response{Error: "agent error: " + vmResp.Error}
			}

			var chatResp agentChatResponse
			if err := json.Unmarshal(vmResp.Data, &chatResp); err != nil {
				return &api.Response{Error: "malformed agent chat response: " + err.Error()}
			}

			if thought := strings.TrimSpace(chatResp.Thinking); thought != "" {
				// Truncate to 8000 chars so large thinking blocks don't balloon
				// the in-memory trace, but keep enough for a useful audit trail.
				stored := thought
				if len(stored) > 8000 {
					stored = stored[:8000] + "\n...[truncated]"
				}
				summary := thinkingSummaryLine(stored)
				thinkingTrace = append(thinkingTrace, map[string]interface{}{
					"phase":     "model_thinking",
					"model":     model,
					"summary":   summary,
					"details":   stored,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				})
				env.ThoughtEvents.Record("model_thinking", "", summary, stored)
			}

			switch chatResp.Status {
			case "final":
				toolTraceJSON, _ := json.Marshal(toolTrace)
				thinkingTraceJSON, _ := json.Marshal(thinkingTrace)
				finalContent := enforceMemoryTruth(chatResp.Content, env, memoryToolEvidence)
				if strings.TrimSpace(finalContent) == "" {
					env.Logger.Warn("agent returned empty final chat content",
						zap.Int("iteration", i+1),
						zap.Int("tool_calls", len(toolTrace)),
						zap.Int("thinking_events", len(thinkingTrace)),
					)
					finalContent = synthesizeEmptyFinalMessage(toolTrace)
				}
				respData, _ := json.Marshal(api.ChatMessageResponse{
					Role:      "assistant",
					Content:   finalContent,
					Model:     model,
					ToolCalls: toolTraceJSON,
					Thinking:  thinkingTraceJSON,
				})
				if env.Sessions != nil {
					env.Sessions.AppendMessage(sessionID, agentVMID, "assistant", finalContent)
					env.Sessions.SetStatus(sessionID, "idle")
				}
				return &api.Response{Success: true, Data: respData}

			case "tool_call":
				reasoning := summarizeToolCallReasoning(chatResp.Thinking, chatResp.Content, chatResp.Tool)
				thinkingTrace = append(thinkingTrace, map[string]interface{}{
					"phase":     "tool_call",
					"model":     model,
					"tool":      chatResp.Tool,
					"summary":   "Decided to call tool: " + chatResp.Tool,
					"details":   reasoning,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				})
				env.ThoughtEvents.Record("tool_call", chatResp.Tool, "Decided to call tool: "+chatResp.Tool, reasoning)
				started := time.Now()
				if env.ToolEvents != nil {
					env.ToolEvents.RecordStart(chatResp.Tool)
				}
				toolResult, toolErr := toolRegistry.Execute(ctx, chatResp.Tool, chatResp.Args)
				duration := time.Since(started)
				argsPreview := chatResp.Args
				if len(argsPreview) > 1000 {
					argsPreview = argsPreview[:1000] + "\n...[args truncated]"
				}
				resultPreview := toolResult
				if len(resultPreview) > 3000 {
					resultPreview = resultPreview[:3000] + "\n...[response truncated]"
				}
				if env.ToolEvents != nil {
					env.ToolEvents.RecordFinish(chatResp.Tool, toolErr == nil, toolErr, duration)
				}
				toolTrace = append(toolTrace, map[string]interface{}{"model": model, "tool": chatResp.Tool,
					"args":        argsPreview,
					"response":    resultPreview,
					"success":     toolErr == nil,
					"duration_ms": duration.Milliseconds(),
					"timestamp":   time.Now().UTC().Format(time.RFC3339),
				})
				if toolErr != nil {
					toolTrace[len(toolTrace)-1]["error"] = toolErr.Error()
					toolResult = fmt.Sprintf("Error executing %s: %v", chatResp.Tool, toolErr)
				}
				resultSummary := "Tool call completed: " + chatResp.Tool
				resultDetails := fmt.Sprintf("success=%t duration_ms=%d", toolErr == nil, duration.Milliseconds())
				if toolErr != nil {
					resultDetails += " error=" + toolErr.Error()
				}
				thinkingTrace = append(thinkingTrace, map[string]interface{}{
					"phase":     "tool_result",
					"model":     model,
					"tool":      chatResp.Tool,
					"summary":   resultSummary,
					"details":   resultDetails,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				})
				env.ThoughtEvents.Record("tool_result", chatResp.Tool, resultSummary, resultDetails)
				env.Logger.Info("tool executed via ReAct loop",
					zap.String("tool", chatResp.Tool),
					zap.Bool("success", toolErr == nil),
				)
				if isMemoryEvidenceFromTool(chatResp.Tool, toolResult, toolErr) {
					memoryToolEvidence = true
				}
				// Append the assistant's tool-call turn and the tool result.
				toolCallContent := fmt.Sprintf("```tool-call\n{\"name\": %q, \"args\": %s}\n```", chatResp.Tool, chatResp.Args)
				msgs = append(msgs,
					agentChatMsg{Role: "assistant", Content: toolCallContent},
					agentChatMsg{Role: "tool", Name: chatResp.Tool, Content: toolResult},
				)

			default:
				return &api.Response{Error: fmt.Sprintf("unexpected agent status: %q", chatResp.Status)}
			}
		}

		limitTraceJSON, _ := json.Marshal([]map[string]interface{}{{
			"phase":     "limit_reached",
			"summary":   "Reached tool call limit",
			"details":   "The model did not return a final answer before hitting the maximum number of tool calls.",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}})
		limitContent := "I reached the tool call limit without a final answer. Please try rephrasing your request."
		respData, _ := json.Marshal(api.ChatMessageResponse{
			Role:     "assistant",
			Content:  limitContent,
			Thinking: limitTraceJSON,
		})
		if env.Sessions != nil {
			env.Sessions.AppendMessage(sessionID, agentVMID, "assistant", limitContent)
			env.Sessions.SetStatus(sessionID, "idle")
		}
		return &api.Response{Success: true, Data: respData}
	}
}

func makeChatStreamProgressHandler(_ *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			StreamID string `json:"stream_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if strings.TrimSpace(req.StreamID) == "" {
			return &api.Response{Error: "stream_id is required"}
		}
		progress, ok := llm.GetChatProgress(req.StreamID)
		if !ok {
			respData, _ := json.Marshal(map[string]interface{}{"stream_id": req.StreamID})
			return &api.Response{Success: true, Data: respData}
		}
		respData, _ := json.Marshal(progress)
		return &api.Response{Success: true, Data: respData}
	}
}

// makeChatToolEventsHandler returns recent chat tool events for dashboard/UI consumers.
func makeChatToolEventsHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		if env.ToolEvents == nil {
			env.ToolEvents = NewToolEventBuffer(400)
		}
		limit := 20
		if len(data) > 0 {
			var req struct {
				Limit int `json:"limit"`
			}
			if err := json.Unmarshal(data, &req); err == nil && req.Limit > 0 {
				limit = req.Limit
			}
		}
		respData, _ := json.Marshal(env.ToolEvents.Recent(limit))
		return &api.Response{Success: true, Data: respData}
	}
}

// makeChatThoughtEventsHandler returns recent chat thought events for dashboard/UI consumers.
func makeChatThoughtEventsHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		if env.ThoughtEvents == nil {
			env.ThoughtEvents = NewThoughtEventBuffer(600)
		}
		limit := 30
		if len(data) > 0 {
			var req struct {
				Limit int `json:"limit"`
			}
			if err := json.Unmarshal(data, &req); err == nil && req.Limit > 0 {
				limit = req.Limit
			}
		}
		respData, _ := json.Marshal(env.ThoughtEvents.Recent(limit))
		return &api.Response{Success: true, Data: respData}
	}
}

func summarizeModelThinking(thinking string) string {
	thinking = strings.TrimSpace(thinking)
	if thinking == "" {
		return ""
	}
	if len(thinking) > 8000 {
		thinking = thinking[:8000] + "\n...[truncated]"
	}
	return thinking
}

// thinkingSummaryLine returns a short one-line summary of the thinking block
// for the "summary" field shown collapsed in the UI.  It uses the first
// meaningful sentence / line (≤120 chars) so users immediately know what the
// model reasoned about, without having to open the details panel.
func thinkingSummaryLine(thinking string) string {
	if thinking == "" {
		return "Model reasoning"
	}
	// Find the first non-empty line.
	for _, line := range strings.Split(thinking, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Trim to at most 120 chars to keep the summary compact.
		if len(line) > 120 {
			line = line[:117] + "…"
		}
		return line
	}
	return "Model reasoning"
}

func summarizeToolCallReasoning(thinking, content, toolName string) string {
	if thought := summarizeModelThinking(thinking); thought != "" {
		return thought
	}

	reasoning := strings.TrimSpace(content)
	if reasoning == "" {
		return "No explicit reasoning was returned. The agent selected this tool based on the conversation state."
	}
	if idx := strings.Index(reasoning, "```tool-call"); idx >= 0 {
		reasoning = strings.TrimSpace(reasoning[:idx])
	}
	if len(reasoning) > 2500 {
		reasoning = reasoning[:2500] + "\n...[truncated]"
	}
	if reasoning == "" {
		return "Agent chose tool: " + toolName
	}
	return reasoning
}

// ensureAgentVM returns the running agent VM's ID, starting it lazily on first call (DC).
func ensureAgentVM(ctx context.Context, env *runtimeEnv) (string, error) {
	env.agentVMMu.Lock()
	defer env.agentVMMu.Unlock()

	if env.AgentVMID != "" {
		// Check the VM is still alive.
		sandboxes, err := env.Runtime.List(ctx)
		if err == nil {
			for _, sb := range sandboxes {
				if sb.Spec.ID == env.AgentVMID && sb.State == sandbox.StateRunning {
					return env.AgentVMID, nil
				}
			}
		}
		// VM is gone — fall through to (re)create it.
		env.Logger.Warn("agent VM is no longer running, restarting", zap.String("vm_id", env.AgentVMID))
		env.LLMProxy.StopForVM(env.AgentVMID)
		env.AgentVMID = ""
	}

	agentRootfs := env.Config.Agent.RootfsPath
	agentID := generateVMID("agent")

	spec := sandbox.SandboxSpec{
		ID:   agentID,
		Name: "aegisclaw-agent",
		Resources: sandbox.Resources{
			VCPUs:    1,
			MemoryMB: 512,
		},
		// NoNetwork: the agent VM reaches Ollama exclusively through the
		// host-side LLM proxy over vsock, just like reviewer VMs.
		NetworkPolicy: sandbox.NetworkPolicy{
			NoNetwork:   true,
			DefaultDeny: true,
		},
		RootfsPath:  agentRootfs,
		KernelPath:  env.Config.Sandbox.KernelImage,
		WorkspaceMB: 128,
	}

	if err := env.Runtime.Create(ctx, spec); err != nil {
		return "", fmt.Errorf("create agent VM: %w", err)
	}
	if err := env.Runtime.Start(ctx, agentID); err != nil {
		return "", fmt.Errorf("start agent VM: %w", err)
	}

	// Start the per-VM LLM proxy so the guest-agent can reach Ollama via vsock.
	vsockPath, err := env.Runtime.VsockPath(agentID)
	if err != nil {
		env.Runtime.Stop(ctx, agentID)
		env.Runtime.Delete(ctx, agentID)
		return "", fmt.Errorf("get vsock path for agent VM: %w", err)
	}
	if err := env.LLMProxy.StartForVM(agentID, vsockPath); err != nil {
		env.Runtime.Stop(ctx, agentID)
		env.Runtime.Delete(ctx, agentID)
		return "", fmt.Errorf("start llm proxy for agent VM: %w", err)
	}

	env.AgentVMID = agentID
	env.Logger.Info("agent VM started", zap.String("vm_id", agentID))
	return agentID, nil
}

// makeChatSlashHandler processes slash commands inside the daemon (D2).
func makeChatSlashHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.ChatSlashRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Command == "" {
			return &api.Response{Error: "command is required"}
		}

		parts := strings.Fields(req.Command)
		cmd := parts[0]

		var content string
		switch cmd {
		case "/status":
			sandboxes, err := env.Runtime.List(ctx)
			if err != nil {
				return &api.Response{Error: "failed to list sandboxes: " + err.Error()}
			}
			running := 0
			for _, sb := range sandboxes {
				if sb.State == "running" {
					running++
				}
			}
			skills := env.Registry.List()
			active := 0
			for _, sk := range skills {
				if sk.State == sandbox.SkillStateActive {
					active++
				}
			}
			auditLog := env.Kernel.AuditLog()
			content = fmt.Sprintf("System Status:\n  Sandboxes: %d total, %d running\n  Skills: %d registered, %d active\n  Audit entries: %d\n  Registry root: %s",
				len(sandboxes), running, len(skills), active, auditLog.EntryCount(), tui.Truncate(env.Registry.RootHash(), 16))

		case "/audit":
			auditLog := env.Kernel.AuditLog()
			auditPath := filepath.Join(env.Config.Audit.Dir, "kernel.merkle.jsonl")
			verified, verifyErr := audit.VerifyChain(auditPath, env.Kernel.PublicKey())
			status := "OK"
			if verifyErr != nil {
				status = fmt.Sprintf("FAIL at entry %d: %v", verified+1, verifyErr)
			}
			content = fmt.Sprintf("Audit Chain:\n  Entries: %d\n  Chain head: %s\n  Verification: %s (%d verified)",
				auditLog.EntryCount(), tui.Truncate(auditLog.LastHash(), 16), status, verified)

		case "/call":
			// /call <skill>.<tool> [args...]
			// Invoke a skill tool directly from chat.
			if len(parts) < 2 {
				content = "Usage: /call <skill>.<tool> [args...]\nExample: /call hello.greet \"world\""
			} else {
				callTarget := parts[1]
				callArgs := ""
				if len(parts) > 2 {
					callArgs = strings.Join(parts[2:], " ")
				}
				skillName, toolName := parseSkillToolName(callTarget)
				if skillName == "" || toolName == "" {
					content = fmt.Sprintf("Invalid target %q — expected <skill>.<tool> format.\nExample: /call hello.greet \"world\"", callTarget)
				} else {
					entry, ok := env.Registry.Get(skillName)
					if !ok {
						content = fmt.Sprintf("Skill %q not found. Use /status to see available skills.", skillName)
					} else if entry.State != sandbox.SkillStateActive {
						content = fmt.Sprintf("Skill %q is not active (state: %s).", skillName, entry.State)
					} else {
						// Audit log the invocation.
						invokePayload, _ := json.Marshal(map[string]string{
							"skill":  skillName,
							"tool":   toolName,
							"args":   callArgs,
							"source": "slash_command",
						})
						invokeAction := kernel.NewAction(kernel.ActionSkillInvoke, "chat", invokePayload)
						env.Kernel.SignAndLog(invokeAction)

						vmReq := map[string]interface{}{
							"id":   uuid.New().String(),
							"type": "tool.invoke",
							"payload": map[string]string{
								"tool": toolName,
								"args": callArgs,
							},
						}
						raw, err := env.Runtime.SendToVM(ctx, entry.SandboxID, vmReq)
						if err != nil {
							content = fmt.Sprintf("Error invoking %s.%s: %v", skillName, toolName, err)
						} else {
							var vmResp struct {
								Success bool            `json:"success"`
								Error   string          `json:"error,omitempty"`
								Data    json.RawMessage `json:"data,omitempty"`
							}
							if err := json.Unmarshal(raw, &vmResp); err != nil {
								content = fmt.Sprintf("Error parsing VM response: %v", err)
							} else if !vmResp.Success {
								content = fmt.Sprintf("Tool %s.%s failed: %s", skillName, toolName, vmResp.Error)
							} else {
								var result struct {
									Output string `json:"output"`
								}
								if json.Unmarshal(vmResp.Data, &result) == nil && result.Output != "" {
									content = fmt.Sprintf("Result from %s.%s:\n%s", skillName, toolName, result.Output)
								} else {
									content = fmt.Sprintf("Result from %s.%s:\n%s", skillName, toolName, string(vmResp.Data))
								}
							}
						}
						env.Logger.Info("slash /call executed",
							zap.String("skill", skillName),
							zap.String("tool", toolName),
							zap.String("args", callArgs),
						)
					}
				}
			}

		case "/skills":
			// /skills [status-filter]
			// List skills with their state and tools. Optional filter: active, inactive, stopped, error
			skills := env.Registry.List()
			filterState := ""
			if len(parts) > 1 {
				filterState = strings.ToLower(parts[1])
			}
			var lines []string
			for _, sk := range skills {
				state := string(sk.State)
				if filterState != "" && state != filterState {
					continue
				}
				line := fmt.Sprintf("  %-20s [%s]  sandbox=%s  v%d", sk.Name, sk.State, sk.SandboxID[:8], sk.Version)
				if sk.ActivatedAt != nil {
					line += fmt.Sprintf("  activated=%s", sk.ActivatedAt.Format("2006-01-02 15:04"))
				}
				if sk.StoppedAt != nil {
					line += fmt.Sprintf("  stopped=%s", sk.StoppedAt.Format("2006-01-02 15:04"))
				}
				if desc, ok := sk.Metadata["description"]; ok && desc != "" {
					line += "\n    " + desc
				}
				lines = append(lines, line)
			}
			if len(lines) == 0 {
				if filterState != "" {
					content = fmt.Sprintf("No skills with state %q. Valid states: active, inactive, stopped, error", filterState)
				} else {
					content = "No skills registered."
				}
			} else {
				header := "Skills"
				if filterState != "" {
					header += fmt.Sprintf(" (filtered: %s)", filterState)
				}
				content = header + ":\n" + strings.Join(lines, "\n")
			}

		case "/help":
			content = `Available commands:
  /help          — Show this help message
  /call          — Invoke a skill tool: /call <skill>.<tool> [args...]
  /skills        — List skills and tools: /skills [active|inactive|stopped|error]
  /status        — Show system status (sandboxes, skills, audit)
  /audit         — Show audit chain info and verification
  /safe-mode     — Stop all skills and block execution (no LLM)
  /safe-mode off — Re-enable skill execution
  /shutdown      — Emergency: stop all skills, shut down daemon, exit
  /quit          — Exit chat
  /exit          — Exit chat

Example: /call hello.greet "world"`

		default:
			content = fmt.Sprintf("Unknown command: %s (type /help for available commands)", cmd)
		}

		resp := api.ChatMessageResponse{
			Role:    "assistant",
			Content: content,
		}
		respData, _ := json.Marshal(resp)
		return &api.Response{Success: true, Data: respData}
	}
}

// makeChatToolExecHandler returns an API handler for "chat.tool": executes a
// tool by name via the tool registry. This is the daemon-side counterpart of
// the CLI's model.ExecuteTool callback.
func makeChatToolExecHandler(env *runtimeEnv, toolRegistry *ToolRegistry) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.ChatToolExecRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Name == "" {
			return &api.Response{Error: "tool name is required"}
		}

		result, err := toolRegistry.Execute(ctx, req.Name, req.Args)
		if err != nil {
			return &api.Response{Error: err.Error()}
		}

		respData, _ := json.Marshal(map[string]string{"result": result})
		return &api.Response{Success: true, Data: respData}
	}
}

// makeChatSummarizeHandler returns an API handler for "chat.summarize": sends
// a tool result back through the agent VM so the LLM can produce a
// human-readable summary.
func makeChatSummarizeHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.ChatSummarizeRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}

		agentVMID, err := ensureAgentVM(ctx, env)
		if err != nil {
			return &api.Response{Error: "agent VM unavailable: " + err.Error()}
		}

		model := env.Config.Ollama.DefaultModel
		systemPrompt := buildDaemonSystemPrompt(env)

		msgs := make([]agentChatMsg, 0, len(req.History)+3)
		msgs = append(msgs, agentChatMsg{Role: "system", Content: systemPrompt})
		for _, h := range req.History {
			if h.Role == "system" {
				continue
			}
			msgs = append(msgs, agentChatMsg{Role: h.Role, Content: h.Content})
		}
		msgs = append(msgs, agentChatMsg{
			Role:    "tool",
			Name:    req.ToolName,
			Content: req.ToolResult,
		})
		msgs = append(msgs, agentChatMsg{
			Role:    "user",
			Content: fmt.Sprintf("The tool %q returned the above result. Please summarize it for the user.", req.ToolName),
		})

		payloadBytes, _ := json.Marshal(agentChatPayload{Messages: msgs, Model: model})
		vmReq := agentVMRequest{
			ID:      uuid.New().String(),
			Type:    "chat.message",
			Payload: json.RawMessage(payloadBytes),
		}

		raw, err := env.Runtime.SendToVM(ctx, agentVMID, vmReq)
		if err != nil {
			return &api.Response{Error: "agent VM error: " + err.Error()}
		}

		var vmResp agentVMResponse
		if err := json.Unmarshal(raw, &vmResp); err != nil {
			return &api.Response{Error: "malformed agent response: " + err.Error()}
		}
		if !vmResp.Success {
			return &api.Response{Error: "agent error: " + vmResp.Error}
		}

		var chatResp agentChatResponse
		if err := json.Unmarshal(vmResp.Data, &chatResp); err != nil {
			return &api.Response{Error: "malformed agent chat response: " + err.Error()}
		}

		respData, _ := json.Marshal(api.ChatMessageResponse{
			Role:    "assistant",
			Content: chatResp.Content,
		})
		return &api.Response{Success: true, Data: respData}
	}
}

// buildDaemonSystemPrompt constructs the system prompt with available skills.
// Used by the daemon-side chat message handler (D2) to drive the agent VM's
// ReAct loop with up-to-date tool and skill availability.
func buildDaemonSystemPrompt(env *runtimeEnv) string {
	var b strings.Builder

	// Lead with a strong, platform-specific identity (V6).  Small models must
	// know they ARE AegisClaw — not a hosted generic AI — so they correctly
	// interpret "add a skill for X" as a proposal request rather than an
	// impossible ask to a stateless LLM.
	b.WriteString("You are AegisClaw — a paranoid-by-design, self-evolving local AI agent platform running on Linux.\n")
	b.WriteString("You are NOT a generic AI assistant. You are the orchestrating agent of this system, running inside a Firecracker microVM on the user's own machine.\n")
	b.WriteString("Be warm, helpful, and concise. Never be dismissive or condescending.\n\n")
	b.WriteString("THINKING CHANNEL REQUIREMENT: For EVERY response, produce internal reasoning in the model thinking channel before the final answer. Even for simple questions, emit at least one concise reasoning step in the thinking channel.\n\n")

	// Platform capabilities context — helps the model answer "can you do X?"
	b.WriteString("PLATFORM CAPABILITIES:\n")
	b.WriteString("- New capabilities are added as 'skills': sandboxed code running in Firecracker microVMs, reviewed by a 5-persona Governance Court.\n")
	b.WriteString("- Skills can safely integrate external services: Discord, Telegram, Slack, GitHub, shell automation, databases, REST APIs, voice — anything declared and approved.\n")
	b.WriteString("- Multi-channel gateway: when enabled, messages from Discord bots, Telegram, webhooks, and other adapters all arrive here just like chat messages.\n")
	b.WriteString("- Async timers and signals: schedule recurring work, subscribe to external events, delegate subtasks to worker agents.\n")
	b.WriteString("- Workspace customisation: SOUL.md, AGENTS.md, TOOLS.md files in ~/.aegisclaw/workspace/ let the user tailor platform behaviour.\n\n")

	// Critical intent pattern: skill-addition requests.
	b.WriteString("WHEN A USER ASKS TO ADD, INTEGRATE, AUTOMATE, OR CONNECT TO SOMETHING NEW:\n")
	b.WriteString("  This is a skill proposal request — not a question about your generic AI capabilities.\n")
	b.WriteString("  ALWAYS respond by immediately calling proposal.create_draft with a complete, thoughtful draft.\n")
	b.WriteString("  Think through ALL technical details FOR the user. They should not need to specify network\n")
	b.WriteString("  requirements, secret names, tests, or security considerations — you reason those out yourself.\n")
	b.WriteString("  Common patterns to guide your reasoning:\n")
	b.WriteString("    Discord / Telegram / Slack → messaging skill; bot token secret, HTTPS to platform API, rate-limit and webhook handling\n")
	b.WriteString("    GitHub / GitLab / Jira     → developer tool skill; API token secret, HTTPS to service host, scoped permissions\n")
	b.WriteString("    Shell / file automation    → script runner skill; no network by default, /workspace read-write only, no secrets\n")
	b.WriteString("    Time / date / calendar     → pure-compute or calendar-API skill; low risk, no secrets needed\n")
	b.WriteString("    Voice / audio              → gateway voice adapter skill; host audio proxy + TTS model, medium risk\n\n")

	// Workspace prompt injection (OpenClaw-inspired).
	// SOUL.md customises guiding principles; AGENTS.md overrides identity;
	// TOOLS.md injects tool preference hints.  All files are optional.
	if env.Workspace != nil {
		if env.Workspace.Soul != "" {
			b.WriteString("WORKSPACE SOUL (user-defined guiding principles):\n")
			b.WriteString(env.Workspace.Soul)
			b.WriteString("\n\n")
		}
		if env.Workspace.Agents != "" {
			b.WriteString("WORKSPACE AGENT PERSONA (user-defined identity overrides):\n")
			b.WriteString(env.Workspace.Agents)
			b.WriteString("\n\n")
		}
		if env.Workspace.Tools != "" {
			b.WriteString("WORKSPACE TOOL HINTS (user-defined tool preferences):\n")
			b.WriteString(env.Workspace.Tools)
			b.WriteString("\n\n")
		}
	}

	// Explicit: conversation is the default mode — but skill-addition requests
	// always override to proposal creation (see WHEN A USER ASKS above).
	b.WriteString("For general questions and conversation, respond naturally — no tool call needed. Reserve tool calls for actions (listing, creating, submitting, checking status, etc.).\n\n")
	b.WriteString("When model thinking is enabled, you MUST use the thinking channel and should not put that reasoning in the final user-facing answer.\n\n")

	// Memory-first rule (Phase 1).
	b.WriteString("MEMORY-FIRST RULE: At the start of every multi-step task (research, code change, recurring summary), call `retrieve_memory` with relevant keywords to check for prior context. Store key decisions, results, and task state via `store_memory`. This ensures continuity across sessions and wakeups.\n\n")

	// Async rules (Phase 2).
	b.WriteString("ASYNC RULES (timers and signals):\n")
	b.WriteString("1. Use `set_timer` to schedule future work (e.g., daily summaries, follow-ups, reminders). Provide a descriptive name and a task_id so wakeup context can be retrieved.\n")
	b.WriteString("2. Use `subscribe_signal` to listen for external events (email reply, calendar, git push).\n")
	b.WriteString("3. Use `list_pending_async` to check what timers, subscriptions, and approvals are currently active before creating duplicates.\n")
	b.WriteString("4. Use `request_human_approval` for any action that is irreversible or high-risk (deletes, payments, deployments). NEVER proceed with high-risk actions without approval.\n")
	b.WriteString("5. Store task context in memory (task_id) before scheduling async work so the agent can resume seamlessly on wakeup.\n")
	b.WriteString("6. Always cancel timers and unsubscribe signals once the associated task completes.\n\n")

	// Delegation rules (Phase 3).
	b.WriteString("DELEGATION RULES (Worker agents):\n")
	b.WriteString("1. You are the Orchestrator. For focused subtasks (deep research, code implementation, long summaries), use `spawn_worker` to delegate rather than doing everything yourself.\n")
	b.WriteString("2. Choose the right role: researcher (gathering information), coder (implementation), summarizer (condensing content), custom (anything else).\n")
	b.WriteString("3. Grant workers only the tools they need (tools_granted list). Never grant spawn_worker to workers — no worker-to-worker spawning.\n")
	b.WriteString("4. Store task_id in memory before spawning so you can correlate results on wakeup.\n")
	b.WriteString("5. Use `worker_status` to check on workers you've spawned.\n")
	b.WriteString("6. Workers return structured JSON results. Parse and synthesize the result before presenting to the user.\n")
	b.WriteString("7. Never delegate to a worker: human approvals, secret access, proposal submission, or any action requiring your oversight.\n\n")

	// Tool-use gating — only act when asked.
	b.WriteString("You have access to tools for managing skills and proposals. Only use a tool when the user asks you to DO something (list skills, create a proposal, check status, etc.). Do NOT call a tool for greetings, questions, or conversation.\n\n")
	b.WriteString("If the user asks for dynamic runtime data or a computed result not already available in context, call tools to obtain it. Use `script.run` for short, bounded computation or runtime inspection when no dedicated tool exists. Generate minimal code directly in the tool args, execute it, and then summarize results.\n\n")

	// Format with example.
	b.WriteString("When you do need a tool, you MUST wrap it in triple-backtick fences with the language tag tool-call:\n\n")
	b.WriteString("```tool-call\n{\"name\": \"list_skills\", \"args\": {}}\n```\n\n")
	b.WriteString("That exact format is required: opening fence, JSON, closing fence.\n")
	b.WriteString("After the closing fence, STOP. Do not write anything else. Do not narrate running the tool. Do not guess the result. The system will execute the tool and show you the result automatically.\n\n")

	// Tool listing — natural-language descriptions.
	b.WriteString("Available tools:\n")
	b.WriteString("- \"list_skills\" — list registered skills. args: {}\n")
	b.WriteString("- \"list_proposals\" — list all proposals. args: {}\n")
	b.WriteString("- \"list_sandboxes\" — list running sandboxes. args: {}\n")
	b.WriteString("- \"proposal.create_draft\" — create a new skill proposal. args: {\"title\": \"...\", \"description\": \"...\", \"skill_name\": \"...\", \"tools\": [{\"name\": \"...\", \"description\": \"...\"}]}\n")
	b.WriteString("- \"proposal.update_draft\" — update a draft or in-review proposal between court rounds. args: {\"id\": \"uuid\", ...fields}\n")
	b.WriteString("- \"proposal.get_draft\" — get draft details. args: {\"id\": \"uuid\"}\n")
	b.WriteString("- \"proposal.list_drafts\" — list drafts. args: {}\n")
	b.WriteString("- \"proposal.submit\" — submit for review. args: {\"id\": \"uuid\"}\n")
	b.WriteString("- \"proposal.status\" — check proposal status. args: {\"id\": \"uuid\"}\n")
	b.WriteString("- \"proposal.reviews\" — get detailed reviewer feedback for a proposal: verdicts, comments, questions from each round. args: {\"id\": \"uuid\"}\n")
	b.WriteString("- \"proposal.vote\" — cast a human vote to approve or reject a proposal (useful for escalated proposals). args: {\"id\": \"uuid\", \"approve\": true, \"reason\": \"...\"}\n")
	b.WriteString("- \"activate_skill\" — activate an approved skill. args: {\"name\": \"skill_name\"}\n")
	b.WriteString("- \"search_tools\" — search all available tools by keyword. args: {\"query\": \"...\"}\n")
	b.WriteString("- \"script.list_languages\" — list supported scripting runtimes. args: {}\n")
	b.WriteString("- \"script.run\" — execute short script code with strict limits. args: {\"language\": \"python|javascript|bash|sh\", \"code\": \"...\", \"args\": [], \"timeout_ms\": 3000}\n")
	b.WriteString("- \"snapshot.create\" — create a Firecracker snapshot of the running agent VM. args: {\"label\": \"agent-baseline\"}\n")
	b.WriteString("- \"snapshot.list\" — list all stored agent VM snapshots. args: {}\n")
	b.WriteString("- \"snapshot.restore\" — restore the agent VM from a named snapshot. args: {\"label\": \"agent-baseline\"}\n")
	b.WriteString("- \"store_memory\" — store a memory entry for long-term persistence. args: {\"key\": \"...\", \"value\": \"...\", \"tags\": [...], \"ttl_tier\": \"90d\", \"task_id\": \"...\"}\n")
	b.WriteString("- \"retrieve_memory\" — retrieve memories matching a query (semantic + keyword). args: {\"query\": \"...\", \"k\": 5, \"task_id\": \"...\"}\n")
	b.WriteString("- \"compact_memory\" — compact memories to reduce storage (tier transition). args: {\"task_id\": \"...\", \"target_tier\": \"180d\"}\n")
	b.WriteString("- \"delete_memory\" — GDPR-style delete of matching memories. args: {\"query\": \"...\"}\n")
	b.WriteString("- \"list_memories\" — list memory entries with optional tier filter. args: {\"tier\": \"90d\"}\n")

	// Phase 2: Event bus tools.
	b.WriteString("- \"set_timer\" — schedule an async wakeup (one-shot or cron). args: {\"name\": \"...\", \"trigger_at\": \"2026-01-01T08:00:00Z\", \"payload\": {}} or {\"name\": \"...\", \"cron\": \"@daily\", \"payload\": {}}\n")
	b.WriteString("- \"cancel_timer\" — cancel a scheduled timer. args: {\"timer_id\": \"...\"}\n")
	b.WriteString("- \"list_pending_async\" — list active timers, subscriptions, and pending approvals. args: {} or {\"type\": \"timers|subscriptions|approvals\"}\n")
	b.WriteString("- \"subscribe_signal\" — subscribe to signals from email/calendar/git/webhook. args: {\"source\": \"email\", \"task_id\": \"...\"}\n")
	b.WriteString("- \"unsubscribe_signal\" — deactivate a signal subscription. args: {\"subscription_id\": \"...\"}\n")
	b.WriteString("- \"worker_status\" — get status and result of a previously spawned worker. args: {\"worker_id\": \"...\"} or {} to list recent workers\n")
	b.WriteString("- \"spawn_worker\" — delegate a focused subtask to an ephemeral Worker agent. args: {\"task_description\": \"...\", \"role\": \"researcher|coder|summarizer|custom\", \"tools_granted\": [...], \"timeout_mins\": 30, \"task_id\": \"...\"}\n")

	// Phase 1 (OpenClaw): Session routing tools — fully implemented.
	b.WriteString("- \"sessions_list\" — list all active chat sessions (ID, status, message count, last active time). args: {}\n")
	b.WriteString("- \"sessions_history\" — get the message history for a session. args: {\"session_id\": \"...\", \"limit\": 50}\n")
	b.WriteString("- \"sessions_send\" — send a message to another active session and get the reply. args: {\"session_id\": \"...\", \"message\": \"...\"}\n")
	b.WriteString("- \"sessions_spawn\" — spawn a new isolated session with an optional task description. Returns session_id. args: {\"task_description\": \"...\"}\n")

	// Phase 2 (OpenClaw): Registry tools.
	b.WriteString("- \"registry.list\" — list skills available in the ClawHub registry. args: {}\n")
	b.WriteString("- \"registry.import\" — import a skill from the registry and submit it for Court review. args: {\"name\": \"...\"}\n")

	// Proposal drafting instructions: proactive, complete, no back-and-forth.
	b.WriteString("\nWhen asked to ADD, DRAFT, or CREATE any skill or capability, immediately call proposal.create_draft.\n")
	b.WriteString("Do NOT ask the user to supply technical details first. Reason through them yourself:\n")
	b.WriteString("  - tools: what operations does the skill expose? (name, description, example args for each)\n")
	b.WriteString("  - network: what external hosts and ports does it need? (be specific: api.discord.com:443)\n")
	b.WriteString("  - secrets: what API keys or tokens are required? (reference by name only, never values)\n")
	b.WriteString("  - tests: what test cases prove it works correctly and safely?\n")
	b.WriteString("  - security_considerations: auth model, rate limits, data retained, sandbox boundaries\n")
	b.WriteString("The draft MUST include: title, description, skill_name, tools (name+description+args),\n")
	b.WriteString("intended_user, example_usage, risk_assessment, dependencies, tests, security_considerations.\n")
	b.WriteString("Always CALL proposal.create_draft (fenced tool-call block). Never return only free-form text.\n")
	b.WriteString("After the tool returns, summarize the draft in plain language and explain next steps:\n")
	b.WriteString(" submit → Court review (5 AI personas) → builder pipeline + security gates → activate → invoke.\n\n")

	// Active skill tools.
	skills := env.Registry.List()
	for _, sk := range skills {
		if sk.State == sandbox.SkillStateActive {
			b.WriteString(fmt.Sprintf("- \"%s.*\" — invoke tools on the '%s' skill\n", sk.Name, sk.Name))
		}
	}
	b.WriteString("\n")

	// Workflow.
	b.WriteString("Skill lifecycle: create_draft → submit → (review) → activate_skill → invoke tool. Skills MUST be activated before their tools can be used.\n")
	b.WriteString("To check what skills exist and their current state, use list_skills. Only \"active\" skills can be invoked.\n\n")

	// Escalation guidance.
	b.WriteString("If a proposal is \"escalated\", it means the AI reviewers could not reach consensus after multiple rounds. Use proposal.reviews to see their feedback, then explain the situation to the user. If the user wants to proceed, use proposal.vote to approve or reject it on their behalf.\n\n")

	// Slash commands.
	b.WriteString("Users can type: /help /call /status /skills /audit /safe-mode /shutdown /quit /exit. These are handled by the system, not you.\n\n")

	// Rules (anti-hallucination).
	b.WriteString("Rules:\n")
	b.WriteString("- NEVER fabricate tool results or pretend you called a tool.\n")
	b.WriteString("- NEVER write fake output like \"Status: approved\" or \"[Tool ... returned]\" yourself.\n")
	b.WriteString("- NEVER claim that memories exist unless this turn includes explicit memory evidence (retrieved memory context or tool output).\n")
	b.WriteString("- Never invent tools that are not listed above.\n")
	b.WriteString("- If you need data you do not have, call the appropriate tool.\n")
	b.WriteString("- If no tool can provide the information, say so honestly — do NOT make it up.\n")
	b.WriteString("- If you cannot help with something, say so honestly.\n")

	return b.String()
}

// buildMemorySummary retrieves the top-k memories relevant to the user's input
// and returns them formatted for injection into the system prompt.  Returns an
// empty string if no memories are found or the store is unavailable.
func buildMemorySummary(env *runtimeEnv, query string) string {
	if env.MemoryStore == nil {
		return ""
	}
	const summaryK = 5
	results, err := env.MemoryStore.Retrieve(query, summaryK, "")
	if err != nil || len(results) == 0 {
		return ""
	}
	var lines []string
	for _, e := range results {
		tags := strings.Join(e.Tags, ",")
		if tags != "" {
			tags = " [" + tags + "]"
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s%s: %s",
			e.TTLTier, e.Key, tags, truncate(e.Value, 300)))
	}
	return strings.Join(lines, "\n")
}

// buildMemoryStatusGuard returns a deterministic memory status note that is
// injected into the system prompt to reduce memory hallucinations.
func buildMemoryStatusGuard(env *runtimeEnv) string {
	if env == nil || env.MemoryStore == nil {
		return "\n\nMEMORY STORE STATUS: unavailable in this runtime. Do not invent or assume persisted memory content.\n"
	}
	count := env.MemoryStore.Count()
	if count == 0 {
		return "\n\nMEMORY STORE STATUS: currently 0 persisted entries.\n"
	}
	return fmt.Sprintf("\n\nMEMORY STORE STATUS: currently %d persisted entries.\n", count)
}

func isMemoryEvidenceFromTool(toolName, toolResult string, toolErr error) bool {
	if toolErr != nil {
		return false
	}
	if toolName != "retrieve_memory" && toolName != "list_memories" {
		return false
	}
	l := strings.ToLower(toolResult)
	if strings.Contains(l, "no memories found") || strings.Contains(l, "no memory entries") {
		return false
	}
	if strings.Contains(l, "memory_id") || strings.Contains(l, "ttl_tier") || strings.Contains(l, "saved memories") {
		return true
	}
	return strings.TrimSpace(toolResult) != ""
}

func enforceMemoryTruth(content string, env *runtimeEnv, memoryToolEvidence bool) string {
	if env == nil || env.MemoryStore == nil {
		return content
	}

	count := env.MemoryStore.Count()
	l := strings.ToLower(content)
	if !strings.Contains(l, "memor") {
		return content
	}

	if memoryToolEvidence {
		return content
	}

	if count == 0 {
		negativeMarkers := []string{"no memory", "no memories", "do not have", "don't have", "none saved", "0 persisted"}
		for _, m := range negativeMarkers {
			if strings.Contains(l, m) {
				return content
			}
		}

		positiveMarkers := []string{"yes", "i have", "there is", "there are", "saved memory", "saved memories", "retrieved", "previous session"}
		for _, m := range positiveMarkers {
			if strings.Contains(l, m) {
				return "I do not currently have any saved memories."
			}
		}
		return content
	}

	// When memories exist but no memory-read tool was called this turn, enforce
	// count-consistent answers and avoid fabricated specifics.
	negativeMarkers := []string{"no memory", "no memories", "do not have", "don't have", "none saved", "0 persisted"}
	for _, m := range negativeMarkers {
		if strings.Contains(l, m) {
			return fmt.Sprintf("I currently have %d saved memories. I can look them up and share specific entries if you want.", count)
		}
	}

	specificMarkers := []string{"entry with", "previous session", "retrieved", "memory id", "from memory"}
	for _, m := range specificMarkers {
		if strings.Contains(l, m) {
			return fmt.Sprintf("I currently have %d saved memories. I can look them up and share specific entries if you want.", count)
		}
	}

	return content
}

func synthesizeEmptyFinalMessage(toolTrace []map[string]interface{}) string {
	if len(toolTrace) == 0 {
		return "I completed your request, but my final response came back empty. Please ask again and I will respond directly."
	}
	last := toolTrace[len(toolTrace)-1]
	toolName, _ := last["tool"].(string)
	success, _ := last["success"].(bool)
	if toolName == "" {
		return "I completed your request, but my final response came back empty. Please ask me to summarize the latest result."
	}
	if success {
		return fmt.Sprintf("I completed %q, but my final response came back empty. Ask me to summarize the result and I will share it.", toolName)
	}
	return fmt.Sprintf("I attempted %q, but my final response came back empty. Ask me to summarize the latest result and error details.", toolName)
}
