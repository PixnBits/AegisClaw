package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/audit"
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

	model.SendMessage = func(input string, history []tui.ChatMessage) (tui.ChatMessage, []tui.ToolCall, error) {
		// Handle slash commands
		if strings.HasPrefix(input, "/") {
			return handleSlashCommand(env, input)
		}

		// Default: echo with context-aware response
		return tui.ChatMessage{
			Role:      tui.ChatRoleAssistant,
			Content:   fmt.Sprintf("Received: %q. Use /propose, /status, /audit, /court for system operations. Full LLM integration available in Epic 6.", input),
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
