package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/audit"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/tui"
)

// makeChatMessageHandler returns an API handler that processes chat messages
// inside the daemon process (D2).
//
// This is the core D2 change: the host CLI process no longer calls Ollama
// directly. Instead, all LLM interaction for chat is routed through the
// daemon, which is the only component authorized to plan tool usage.
func makeChatMessageHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.ChatMessageRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Input == "" {
			return &api.Response{Error: "input is required"}
		}

		ollamaClient := llm.NewClient(llm.ClientConfig{
			Endpoint: env.Config.Ollama.Endpoint,
		})

		// Build the system prompt with available skills.
		systemPrompt := buildDaemonSystemPrompt(env)

		// Convert history to Ollama messages.
		msgs := []llm.ChatMessage{{Role: "system", Content: systemPrompt}}
		for _, h := range req.History {
			if h.Role == "system" {
				continue
			}
			msgs = append(msgs, llm.ChatMessage{Role: h.Role, Content: h.Content})
		}
		msgs = append(msgs, llm.ChatMessage{Role: "user", Content: req.Input})

		resp, err := ollamaClient.Chat(ctx, llm.ChatRequest{
			Model:    env.Config.Ollama.DefaultModel,
			Messages: msgs,
		})
		if err != nil {
			return &api.Response{Error: "ollama: " + err.Error()}
		}

		content := resp.Message.Content

		// Check for tool call patterns in the response.
		chatResp := api.ChatMessageResponse{
			Role:    "assistant",
			Content: content,
		}

		respData, _ := json.Marshal(chatResp)
		return &api.Response{Success: true, Data: respData}
	}
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

		case "/help":
			content = `Available commands:
  /help          — Show this help message
  /status        — Show system status (sandboxes, skills, audit)
  /audit         — Show audit chain info and verification
  /court         — List court sessions / proposals
  /propose       — Start building a new skill proposal (interactive)
  /safe-mode     — Stop all tools and skills immediately (no LLM)
  /safe-mode off — Re-enable tool and skill execution
  /shutdown      — Emergency: stop all skills, shut down daemon, exit
  /quit          — Exit chat`

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

// makeChatToolHandler executes a tool call inside the daemon (D2).
func makeChatToolHandler(env *runtimeEnv) api.Handler {
	daemonClient := api.NewClient(env.Config.Daemon.SocketPath)

	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.ChatToolExecRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Name == "" {
			return &api.Response{Error: "tool name is required"}
		}

		var result string
		var toolErr error

		switch req.Name {
		case "list_proposals":
			summaries, err := env.ProposalStore.List()
			if err != nil {
				return &api.Response{Error: "list proposals: " + err.Error()}
			}
			var lines []string
			for _, s := range summaries {
				lines = append(lines, fmt.Sprintf("  %s  %s  [%s]  %s", s.ID, s.Title, s.Status, s.Risk))
			}
			if len(lines) == 0 {
				result = "No proposals found."
			} else {
				result = strings.Join(lines, "\n")
			}

		case "list_sandboxes":
			sandboxes, err := env.Runtime.List(ctx)
			if err != nil {
				return &api.Response{Error: "list sandboxes: " + err.Error()}
			}
			var lines []string
			for _, sb := range sandboxes {
				lines = append(lines, fmt.Sprintf("  %s  %s  [%s]", sb.Spec.ID[:8], sb.Spec.Name, sb.State))
			}
			if len(lines) == 0 {
				result = "No sandboxes found."
			} else {
				result = strings.Join(lines, "\n")
			}

		default:
			// Route skill tool calls through the daemon API.
			skill, tool := parseSkillTool(req.Name)
			if skill != "" && tool != "" {
				resp, err := daemonClient.Call(ctx, "skill.invoke", api.SkillInvokeRequest{
					Skill: skill,
					Tool:  tool,
					Args:  req.Args,
				})
				if err != nil {
					return &api.Response{Error: "skill invoke: " + err.Error()}
				}
				if !resp.Success {
					return &api.Response{Error: "skill invoke failed: " + resp.Error}
				}
				result = string(resp.Data)
			} else {
				toolErr = fmt.Errorf("unknown tool: %s", req.Name)
			}
		}

		if toolErr != nil {
			return &api.Response{Error: toolErr.Error()}
		}

		respData, _ := json.Marshal(map[string]string{"result": result})
		return &api.Response{Success: true, Data: respData}
	}
}

// makeChatSummarizeHandler summarizes a tool result via LLM inside the daemon (D2).
func makeChatSummarizeHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.ChatSummarizeRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}

		ollamaClient := llm.NewClient(llm.ClientConfig{
			Endpoint: env.Config.Ollama.Endpoint,
		})

		systemPrompt := buildDaemonSystemPrompt(env)
		msgs := []llm.ChatMessage{{Role: "system", Content: systemPrompt}}
		for _, h := range req.History {
			if h.Role == "system" {
				continue
			}
			role := h.Role
			if role == "tool" {
				role = "user"
			}
			msgs = append(msgs, llm.ChatMessage{Role: role, Content: h.Content})
		}

		summarizeInstruction := "Please summarize this result for the user in a natural, conversational way. Do NOT output a tool-call block."
		if req.ToolName == "proposal.create_draft" {
			summarizeInstruction = "A new draft proposal was just created. Present the details to the user including the FULL proposal ID. Ask the user to confirm before you submit it."
		} else if req.ToolName == "proposal.submit" {
			summarizeInstruction = "The proposal was just submitted for court review. Tell the user the result and the proposal ID."
		}

		msgs = append(msgs, llm.ChatMessage{
			Role:    "user",
			Content: fmt.Sprintf("[Tool %s returned]: %s\n%s", req.ToolName, req.ToolResult, summarizeInstruction),
		})

		resp, err := ollamaClient.Chat(ctx, llm.ChatRequest{
			Model:    env.Config.Ollama.DefaultModel,
			Messages: msgs,
		})
		if err != nil {
			return &api.Response{Error: "ollama: " + err.Error()}
		}

		chatResp := api.ChatMessageResponse{
			Role:    "assistant",
			Content: resp.Message.Content,
		}
		respData, _ := json.Marshal(chatResp)
		return &api.Response{Success: true, Data: respData}
	}
}

// buildDaemonSystemPrompt constructs the system prompt with available skills.
// This is used by the daemon-side chat handlers (D2) instead of the old
// host-side buildSystemPrompt.
func buildDaemonSystemPrompt(env *runtimeEnv) string {
	var b strings.Builder
	b.WriteString("You are the AegisClaw main agent — a security-first assistant that manages skills running in isolated Firecracker microVMs.\n\n")
	b.WriteString("Available tools:\n")
	b.WriteString("- list_proposals: List all proposals and their status\n")
	b.WriteString("- list_sandboxes: List running sandboxes\n")
	b.WriteString("- proposal.create_draft: Create a new skill proposal draft\n")
	b.WriteString("- proposal.update_draft: Update fields on a draft proposal\n")
	b.WriteString("- proposal.get_draft: Get the current state of a draft\n")
	b.WriteString("- proposal.list_drafts: List all draft proposals\n")
	b.WriteString("- proposal.submit: Submit a proposal for court review\n")
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
		b.WriteString("No skills are currently active. Use /propose or describe a skill you need.\n")
	}

	b.WriteString("\nTo invoke a tool, respond with a JSON block:\n")
	b.WriteString("```tool-call\n{\"name\": \"tool_name\", \"args\": \"arguments\"}\n```\n")

	return b.String()
}

// parseSkillTool splits "skillname.toolname" into skill and tool parts.
// Defined here for use by chat handlers.
func parseSkillTool(name string) (skill, tool string) {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) != 2 {
		return "", ""
	}
	// Reject known non-skill tool prefixes.
	switch parts[0] {
	case "list", "proposal":
		return "", ""
	}
	return parts[0], parts[1]
}
