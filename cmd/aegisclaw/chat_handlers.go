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
	Messages []agentChatMsg `json:"messages"`
	Model    string         `json:"model"`
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
		msgs = append(msgs, agentChatMsg{Role: "user", Content: req.Input})

		for i := 0; i < reactMaxIterations; i++ {
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

		case "/help":
			content = `Available commands:
  /help          — Show this help message
  /call          — Invoke a skill tool: /call <skill>.<tool> [args...]
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

// buildDaemonSystemPrompt constructs the system prompt with available skills.
// Used by the daemon-side chat message handler (D2) to drive the agent VM's
// ReAct loop with up-to-date tool and skill availability.
func buildDaemonSystemPrompt(env *runtimeEnv) string {
	var b strings.Builder
	b.WriteString("You are the AegisClaw main agent — a security-first assistant that manages skills running in isolated Firecracker microVMs.\n\n")
	b.WriteString("## Available tools\n")
	b.WriteString("- list_proposals: List all proposals and their status\n")
	b.WriteString("- list_sandboxes: List running sandboxes\n")
	b.WriteString("- list_skills: List registered skills and their state\n")
	b.WriteString("- activate_skill: Activate a skill by name (starts its microVM)\n")
	b.WriteString("- proposal.create_draft: Create a new skill proposal draft\n")
	b.WriteString("- proposal.update_draft: Update fields on a draft proposal\n")
	b.WriteString("- proposal.get_draft: Get the current state of a draft\n")
	b.WriteString("- proposal.list_drafts: List all draft proposals\n")
	b.WriteString("- proposal.submit: Submit a proposal for court review (auto-triggers review)\n")
	b.WriteString("- proposal.status: Check the status of a proposal\n\n")

	skills := env.Registry.List()
	activeSkills := 0
	for _, sk := range skills {
		if sk.State == sandbox.SkillStateActive {
			activeSkills++
			b.WriteString(fmt.Sprintf("- %s.*: Invoke tools on the '%s' skill\n", sk.Name, sk.Name))
		}
	}

	if activeSkills == 0 {
		b.WriteString("No skills are currently active. Help the user propose and create one.\n")
	}

	b.WriteString("\n## Skill SDLC workflow\n")
	b.WriteString("To create a new skill:\n")
	b.WriteString("1. Use proposal.create_draft to create a draft with title, description, skill_name, and tools.\n")
	b.WriteString("2. Use proposal.submit to submit it for court review (review runs automatically).\n")
	b.WriteString("3. After approval, use activate_skill to start the skill's microVM.\n")
	b.WriteString("4. Once active, invoke the skill's tools using <skillname>.<toolname> or tell the user: /call <skill>.<tool> [args]\n\n")

	b.WriteString("## Chat slash commands (handled directly, not via tools)\n")
	b.WriteString("Users can type these commands in chat:\n")
	b.WriteString("  /call <skill>.<tool> [args] — Invoke a skill tool directly\n")
	b.WriteString("  /status — System status\n")
	b.WriteString("  /help — List all commands\n\n")

	b.WriteString("## Tool invocation format\n")
	b.WriteString("To invoke a tool, respond with a JSON block:\n")
	b.WriteString("```tool-call\n{\"name\": \"tool_name\", \"args\": \"arguments\"}\n```\n")

	return b.String()
}
