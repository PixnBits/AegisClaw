package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/audit"
	"github.com/PixnBits/AegisClaw/internal/kernel"
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
	Messages         []agentChatMsg `json:"messages"`
	Model            string         `json:"model"`
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
	Status  string `json:"status"`            // "final" | "tool_call"
	Role    string `json:"role,omitempty"`    // present when status=="final"
	Content string `json:"content,omitempty"` // present when status=="final"
	Tool    string `json:"tool,omitempty"`    // present when status=="tool_call"
	Args    string `json:"args,omitempty"`    // present when status=="tool_call"
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

		agentVMID, err := ensureAgentVM(ctx, env)
		if err != nil {
			return &api.Response{Error: "agent VM unavailable: " + err.Error()}
		}

		model := env.Config.Ollama.DefaultModel
		systemPrompt := buildDaemonSystemPrompt(env)

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

		for i := 0; i < reactMaxIterations; i++ {
			payloadBytes, _ := json.Marshal(agentChatPayload{
				Messages:         msgs,
				Model:            model,
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

			switch chatResp.Status {
			case "final":
				respData, _ := json.Marshal(api.ChatMessageResponse{
					Role:    "assistant",
					Content: chatResp.Content,
				})
				return &api.Response{Success: true, Data: respData}

			case "tool_call":
				toolResult, toolErr := toolRegistry.Execute(ctx, chatResp.Tool, chatResp.Args)
				if toolErr != nil {
					toolResult = fmt.Sprintf("Error executing %s: %v", chatResp.Tool, toolErr)
				}
				env.Logger.Info("tool executed via ReAct loop",
					zap.String("tool", chatResp.Tool),
					zap.Bool("success", toolErr == nil),
				)
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

		respData, _ := json.Marshal(api.ChatMessageResponse{
			Role:    "assistant",
			Content: "I reached the tool call limit without a final answer. Please try rephrasing your request.",
		})
		return &api.Response{Success: true, Data: respData}
	}
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

	// Lead with conversational identity so small models don't over-constrain
	// themselves to tool-only responses.
	b.WriteString("You are AegisClaw, a friendly and security-conscious coding assistant.\n")
	b.WriteString("You help users manage skills that run in isolated Firecracker microVMs.\n")
	b.WriteString("Be warm, helpful, and concise. Never be dismissive or condescending.\n\n")

	// Explicit: conversation is the default mode.
	b.WriteString("Most of the time, just talk to the user normally. Answer questions, explain things, and be helpful.\n\n")

	// Tool-use gating — only act when asked.
	b.WriteString("You have access to tools for managing skills and proposals. Only use a tool when the user asks you to DO something (list skills, create a proposal, check status, etc.). Do NOT call a tool for greetings, questions, or conversation.\n\n")

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
	b.WriteString("- \"snapshot.create\" — create a Firecracker snapshot of the running agent VM. args: {\"label\": \"agent-baseline\"}\n")
	b.WriteString("- \"snapshot.list\" — list all stored agent VM snapshots. args: {}\n")
	b.WriteString("- \"snapshot.restore\" — restore the agent VM from a named snapshot. args: {\"label\": \"agent-baseline\"}\n")

	// Proposal drafting instructions: tell the agent how to build a court-ready draft
	b.WriteString("\nWhen asked to DRAFT or CREATE a proposal, produce a complete initial\n")
	b.WriteString("proposal that includes the fields the Court requires. At minimum, the\n")
	b.WriteString("draft should include: title, description, skill_name, tools (name+description+args),\n")
	b.WriteString("intended_user, example_usage, risk_assessment, dependencies, tests, and security_considerations.\n")
	b.WriteString("Always prefer to CALL the `proposal.create_draft` tool rather than only returning free-form text.\n")
	b.WriteString("When calling the tool, use a single fenced ```tool-call``` block with JSON args matching those fields.\n")
	b.WriteString("After the tool returns, summarize the created draft in plain language and present a short checklist\n")
	b.WriteString("of items the Court will look for (e.g., tests, risk mitigations, deployment constraints).\n\n")

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
	b.WriteString("- Never invent tools that are not listed above.\n")
	b.WriteString("- If you need data you do not have, call the appropriate tool.\n")
	b.WriteString("- If no tool can provide the information, say so honestly — do NOT make it up.\n")
	b.WriteString("- If you cannot help with something, say so honestly.\n")

	return b.String()
}
