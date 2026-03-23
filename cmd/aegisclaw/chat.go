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
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Interactive ReAct chat interface",
	Long: `Opens an interactive chat interface with persistent context.
Supports slash commands for quick access to AegisClaw operations:
  /propose <goal>  - Start a proposal wizard
  /status          - Show system status
  /audit           - Show audit summary
  /court           - List court sessions
  /quit            - Exit chat`,
	RunE: runChat,
}

func runChat(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	model := tui.NewChatModel()

	// Create Ollama client and daemon API client for skill invocation.
	ollamaClient := llm.NewClient(llm.ClientConfig{
		Endpoint: env.Config.Ollama.Endpoint,
	})
	daemonClient := api.NewClient(env.Config.Daemon.SocketPath)

	model.SendMessage = func(input string, history []tui.ChatMessage) (tui.ChatMessage, []tui.ToolCall, error) {
		// Handle slash commands
		if strings.HasPrefix(input, "/") {
			return handleSlashCommand(env, input)
		}

		// Build the system prompt with available skills.
		systemPrompt := buildSystemPrompt(cmd.Context(), daemonClient)

		// Convert TUI history to Ollama messages.
		msgs := []llm.ChatMessage{{Role: "system", Content: systemPrompt}}
		for _, h := range history {
			if h.Role == tui.ChatRoleSystem {
				continue
			}
			role := "user"
			if h.Role == tui.ChatRoleAssistant {
				role = "assistant"
			}
			msgs = append(msgs, llm.ChatMessage{Role: role, Content: h.Content})
		}
		msgs = append(msgs, llm.ChatMessage{Role: "user", Content: input})

		resp, err := ollamaClient.Chat(cmd.Context(), llm.ChatRequest{
			Model:    env.Config.Ollama.DefaultModel,
			Messages: msgs,
		})
		if err != nil {
			return tui.ChatMessage{}, nil, fmt.Errorf("ollama: %w", err)
		}

		content := resp.Message.Content

		// Check if the LLM wants to invoke a skill tool.
		toolCalls := parseToolCalls(content)
		if len(toolCalls) > 0 {
			// Strip the JSON tool-call block from the displayed message.
			cleaned := cleanToolCallContent(content)
			return tui.ChatMessage{
				Role:      tui.ChatRoleAssistant,
				Content:   cleaned,
				Timestamp: time.Now(),
			}, toolCalls, nil
		}

		return tui.ChatMessage{
			Role:      tui.ChatRoleAssistant,
			Content:   content,
			Timestamp: time.Now(),
		}, nil, nil
	}

	model.ExecuteTool = func(call tui.ToolCall) (string, error) {
		switch call.Name {
		case "list_proposals":
			summaries, err := env.ProposalStore.List()
			if err != nil {
				return "", err
			}
			var lines []string
			for _, s := range summaries {
				lines = append(lines, fmt.Sprintf("  %s  %s  [%s]  %s", s.ID[:8], s.Title, s.Status, s.Risk))
			}
			if len(lines) == 0 {
				return "No proposals found.", nil
			}
			return strings.Join(lines, "\n"), nil

		case "list_sandboxes":
			sandboxes, err := env.Runtime.List(context.Background())
			if err != nil {
				return "", err
			}
			var lines []string
			for _, sb := range sandboxes {
				lines = append(lines, fmt.Sprintf("  %s  %s  [%s]", sb.Spec.ID[:8], sb.Spec.Name, sb.State))
			}
			if len(lines) == 0 {
				return "No sandboxes found.", nil
			}
			return strings.Join(lines, "\n"), nil

		default:
			// Route skill tool calls through the daemon API.
			skill, tool := parseSkillTool(call.Name)
			if skill != "" && tool != "" {
				resp, err := daemonClient.Call(cmd.Context(), "skill.invoke", api.SkillInvokeRequest{
					Skill: skill,
					Tool:  tool,
					Args:  call.Args,
				})
				if err != nil {
					return "", fmt.Errorf("skill invoke: %w", err)
				}
				if !resp.Success {
					return "", fmt.Errorf("skill invoke failed: %s", resp.Error)
				}
				return string(resp.Data), nil
			}
			return "", fmt.Errorf("unknown tool: %s", call.Name)
		}
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func handleSlashCommand(env *runtimeEnv, input string) (tui.ChatMessage, []tui.ToolCall, error) {
	parts := strings.Fields(input)
	cmd := parts[0]

	switch cmd {
	case "/status":
		sandboxes, err := env.Runtime.List(context.Background())
		if err != nil {
			return tui.ChatMessage{}, nil, fmt.Errorf("failed to list sandboxes: %w", err)
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
			if sk.State == "active" {
				active++
			}
		}
		auditLog := env.Kernel.AuditLog()
		content := fmt.Sprintf("System Status:\n  Sandboxes: %d total, %d running\n  Skills: %d registered, %d active\n  Audit entries: %d\n  Registry root: %s",
			len(sandboxes), running, len(skills), active, auditLog.EntryCount(), tui.Truncate(env.Registry.RootHash(), 16))
		return tui.ChatMessage{
			Role:      tui.ChatRoleAssistant,
			Content:   content,
			Timestamp: time.Now(),
		}, nil, nil

	case "/audit":
		auditLog := env.Kernel.AuditLog()
		auditPath := filepath.Join(env.Config.Audit.Dir, "kernel.merkle.jsonl")
		verified, verifyErr := audit.VerifyChain(auditPath, env.Kernel.PublicKey())
		status := "OK"
		if verifyErr != nil {
			status = fmt.Sprintf("FAIL at entry %d: %v", verified+1, verifyErr)
		}
		content := fmt.Sprintf("Audit Chain:\n  Entries: %d\n  Chain head: %s\n  Verification: %s (%d verified)",
			auditLog.EntryCount(), tui.Truncate(auditLog.LastHash(), 16), status, verified)
		return tui.ChatMessage{
			Role:      tui.ChatRoleAssistant,
			Content:   content,
			Timestamp: time.Now(),
		}, nil, nil

	case "/court":
		return tui.ChatMessage{
			Role:      tui.ChatRoleAssistant,
			Content:   "Listing court sessions...",
			Timestamp: time.Now(),
		}, []tui.ToolCall{{Name: "list_proposals"}}, nil

	case "/propose":
		goal := "new skill"
		if len(parts) > 1 {
			goal = strings.Join(parts[1:], " ")
		}
		return tui.ChatMessage{
			Role:      tui.ChatRoleAssistant,
			Content:   fmt.Sprintf("To create a proposal for %q, run: aegisclaw propose skill %q\nThe interactive wizard will guide you through the process.", goal, goal),
			Timestamp: time.Now(),
		}, nil, nil

	default:
		return tui.ChatMessage{
			Role:      tui.ChatRoleAssistant,
			Content:   fmt.Sprintf("Unknown command: %s\nAvailable: /propose, /status, /audit, /court, /quit", cmd),
			Timestamp: time.Now(),
		}, nil, nil
	}
}

// buildSystemPrompt constructs the LLM system prompt including available skills.
func buildSystemPrompt(ctx context.Context, daemonClient *api.Client) string {
	base := `You are AegisClaw, an AI-powered software governance assistant.

You help users manage skills (sandboxed microVM workloads), proposals, and system operations.

Available slash commands (handled locally, not by you):
  /status - Show system status
  /audit  - Show audit chain info
  /court  - List court sessions
  /propose - Start a proposal

When the user asks you to invoke a skill tool, respond with a JSON tool-call block:
` + "```tool-call" + `
{"skill": "<skill-name>", "tool": "<tool-name>", "args": "<arguments>"}
` + "```" + `

`

	// Query daemon for active skills.
	resp, err := daemonClient.Call(ctx, "skill.list", nil)
	if err == nil && resp.Success && len(resp.Data) > 0 {
		var skills []struct {
			Name      string `json:"name"`
			State     string `json:"state"`
			SandboxID string `json:"sandbox_id"`
			Metadata  map[string]string `json:"metadata,omitempty"`
		}
		if json.Unmarshal(resp.Data, &skills) == nil && len(skills) > 0 {
			base += "Currently registered skills:\n"
			for _, s := range skills {
				base += fmt.Sprintf("  - %s (state: %s, sandbox: %s)\n", s.Name, s.State, s.SandboxID[:8])
			}
		}
	}

	return base
}

// parseToolCalls extracts tool-call JSON blocks from LLM output.
func parseToolCalls(content string) []tui.ToolCall {
	var calls []tui.ToolCall
	for {
		start := strings.Index(content, "```tool-call")
		if start < 0 {
			break
		}
		after := content[start+len("```tool-call"):]
		end := strings.Index(after, "```")
		if end < 0 {
			break
		}
		block := strings.TrimSpace(after[:end])
		var tc struct {
			Skill string `json:"skill"`
			Tool  string `json:"tool"`
			Args  string `json:"args"`
		}
		if json.Unmarshal([]byte(block), &tc) == nil && tc.Skill != "" && tc.Tool != "" {
			calls = append(calls, tui.ToolCall{
				Name: tc.Skill + "." + tc.Tool,
				Args: tc.Args,
			})
		}
		content = after[end+3:]
	}
	return calls
}

// cleanToolCallContent removes tool-call blocks from the displayed message.
func cleanToolCallContent(content string) string {
	for {
		start := strings.Index(content, "```tool-call")
		if start < 0 {
			break
		}
		after := content[start+len("```tool-call"):]
		end := strings.Index(after, "```")
		if end < 0 {
			break
		}
		content = content[:start] + after[end+3:]
	}
	return strings.TrimSpace(content)
}

// parseSkillTool splits a "skill.tool" name into its components.
func parseSkillTool(name string) (string, string) {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
