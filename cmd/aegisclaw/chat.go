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
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/tui"
	"github.com/PixnBits/AegisClaw/internal/wizard"
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
  /safe-mode       - Stop all skills and block execution (no LLM)
  /safe-mode off   - Re-enable skill execution
  /shutdown        - Emergency stop: all skills + daemon + exit
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

		case "proposal.create_draft":
			return handleProposalCreateDraft(env, call.Args)

		case "proposal.update_draft":
			return handleProposalUpdateDraft(env, call.Args)

		case "proposal.get_draft":
			return handleProposalGetDraft(env, call.Args)

		case "proposal.list_drafts":
			return handleProposalListDrafts(env)

		case "proposal.submit":
			return handleProposalSubmit(env, daemonClient, cmd.Context(), call.Args)

		case "proposal.status":
			return handleProposalStatus(env, call.Args)

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

	model.SummarizeToolResult = func(toolName, toolResult string, history []tui.ChatMessage) (tui.ChatMessage, error) {
		systemPrompt := buildSystemPrompt(cmd.Context(), daemonClient)
		msgs := []llm.ChatMessage{{Role: "system", Content: systemPrompt}}
		for _, h := range history {
			if h.Role == tui.ChatRoleSystem {
				continue
			}
			role := "user"
			if h.Role == tui.ChatRoleAssistant {
				role = "assistant"
			} else if h.Role == tui.ChatRoleTool {
				role = "user" // Ollama doesn't have a tool role; send as user context
			}
			msgs = append(msgs, llm.ChatMessage{Role: role, Content: h.Content})
		}
		// Add the tool result as context for the LLM.
		msgs = append(msgs, llm.ChatMessage{
			Role:    "user",
			Content: fmt.Sprintf("[Tool %s returned]: %s\nPlease summarize this result for the user in a natural, conversational way. Do NOT output a tool-call block.", toolName, toolResult),
		})

		resp, err := ollamaClient.Chat(cmd.Context(), llm.ChatRequest{
			Model:    env.Config.Ollama.DefaultModel,
			Messages: msgs,
		})
		if err != nil {
			return tui.ChatMessage{}, fmt.Errorf("ollama: %w", err)
		}

		return tui.ChatMessage{
			Role:      tui.ChatRoleAssistant,
			Content:   resp.Message.Content,
			Timestamp: time.Now(),
		}, nil
	}

	model.ToggleSafeMode = func(enable bool) error {
		action := "safe-mode.enable"
		if !enable {
			action = "safe-mode.disable"
		}
		resp, err := daemonClient.Call(cmd.Context(), action, nil)
		if err != nil {
			return fmt.Errorf("daemon: %w", err)
		}
		if !resp.Success {
			return fmt.Errorf("daemon: %s", resp.Error)
		}
		return nil
	}

	model.RequestShutdown = func() error {
		// Enable safe mode first to stop all skills.
		if resp, err := daemonClient.Call(cmd.Context(), "safe-mode.enable", nil); err != nil {
			return fmt.Errorf("safe-mode: %w", err)
		} else if !resp.Success {
			return fmt.Errorf("safe-mode: %s", resp.Error)
		}
		// Request daemon shutdown.
		if resp, err := daemonClient.Call(cmd.Context(), "kernel.shutdown", nil); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		} else if !resp.Success {
			return fmt.Errorf("shutdown: %s", resp.Error)
		}
		return nil
	}

	model.CheckProposalStatus = func(id string) (string, string, error) {
		p, err := env.ProposalStore.Get(id)
		if err != nil {
			return "", "", err
		}
		return string(p.Status), p.Title, nil
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

	case "/help":
		content := `Available commands:
  /help          — Show this help message
  /status        — Show system status (sandboxes, skills, audit)
  /audit         — Show audit chain info and verification
  /court         — List court sessions / proposals
  /propose       — Start building a new skill proposal (interactive)
  /propose <goal>— Start a proposal with a specific goal
  /safe-mode     — Stop all tools and skills immediately (no LLM)
  /safe-mode off — Re-enable tool and skill execution
  /shutdown      — Emergency: stop all skills, shut down daemon, exit
  /quit          — Exit chat

Proposal workflow:
  Describe what you want to build and I'll help you write the proposal.
  Drafts are saved automatically so you can continue later.
  I'll present the full proposal for your approval before submitting.

You can also type natural language to chat with AegisClaw.
Use ↑/↓ arrows to recall previous messages.`
		return tui.ChatMessage{
			Role:      tui.ChatRoleAssistant,
			Content:   content,
			Timestamp: time.Now(),
		}, nil, nil

	case "/propose":
		goal := ""
		if len(parts) > 1 {
			goal = strings.Join(parts[1:], " ")
		}
		if goal != "" {
			return tui.ChatMessage{
				Role:      tui.ChatRoleAssistant,
				Content:   fmt.Sprintf("Let's build a proposal for %q. Tell me more about what this skill should do — what problem does it solve and what tools should it provide?\n\nI'll help you fill in the details and you can save a draft at any point.", goal),
				Timestamp: time.Now(),
			}, nil, nil
		}
		// No goal provided — check for existing drafts.
		return tui.ChatMessage{
			Role:      tui.ChatRoleAssistant,
			Content:   "What would you like to propose? Describe the skill you want to create and I'll help you build the proposal.\n\nOr, if you have an existing draft, tell me the proposal ID and I'll load it.",
			Timestamp: time.Now(),
		}, nil, nil

	default:
		return tui.ChatMessage{
			Role:      tui.ChatRoleAssistant,
			Content:   fmt.Sprintf("Unknown command: %s\nType /help for available commands.", cmd),
			Timestamp: time.Now(),
		}, nil, nil
	}
}

// buildSystemPrompt constructs the LLM system prompt including available skills.
func buildSystemPrompt(ctx context.Context, daemonClient *api.Client) string {
	base := `You are AegisClaw, an AI-powered software governance assistant.

You help users manage skills (sandboxed microVM workloads), proposals, and system operations.

## Slash commands (handled locally, not by you)
  /status     - System status
  /audit      - Audit chain info
  /court      - Court sessions
  /safe-mode  - Emergency stop all skills
  /shutdown   - Emergency daemon shutdown
  /quit       - Exit chat

## Tool invocation format
When you need to call a tool, output EXACTLY this format:
` + "```tool-call" + `
{"skill": "<namespace>", "tool": "<tool-name>", "args": <json-object-or-string>}
` + "```" + `

## Available built-in tools

### Skill tools
For active skills (listed below), use skill name as the "skill" field:
` + "```tool-call" + `
{"skill": "hello-world", "tool": "greet", "args": ""}
` + "```" + `

### Proposal tools (skill = "proposal")
Use these tools to help users create, save, and submit governance proposals.

**proposal.create_draft** — Create a new draft proposal.
  args: {"title": "...", "description": "...", "skill_name": "...", "tools": [{"name": "...", "description": "..."}], "data_sensitivity": 1-5, "network_exposure": 1-5, "privilege_level": 1-5, "allowed_hosts": [], "allowed_ports": [], "secret_refs": []}

**proposal.update_draft** — Update fields on an existing draft.
  args: {"id": "<proposal-id>", ...fields to update (same as create_draft)}

**proposal.get_draft** — Load a proposal by ID.
  args: {"id": "<proposal-id>"}

**proposal.list_drafts** — List all proposals.
  args: {} (no arguments needed)

**proposal.submit** — Submit a draft for court review. The court will then review it.
  args: {"id": "<proposal-id>"}

**proposal.status** — Check current status and review results for a proposal.
  args: {"id": "<proposal-id>"}

## Proposal assistant behavior

When a user wants to create a skill, act as a helpful requirements analyst:

1. **Listen first.** Let the user describe what they want in their own words.
2. **Infer what you can** from the description (e.g. if they mention calling an API, that implies network access).
3. **Never assume.** Ask clarifying questions for anything you're not certain about:
   - What tools should the skill expose? (name and what each does)
   - Does it need network access? If so, which hosts/ports?
   - Does it need secrets (API keys, tokens)?
   - How sensitive is the data it handles? (1=public, 5=highly sensitive)
   - What privilege level does it need? (1=read-only, 5=full system access)
4. **Build incrementally.** You can save a draft at any point so the user can continue later.
5. **Present before submitting.** When all fields are collected, summarize the complete proposal and explicitly ask the user to confirm before calling proposal.submit.
6. **Verify after submission.** After calling proposal.submit, always call proposal.list_drafts to confirm the proposal appears with its new status. Report the verified status to the user.
7. **Show status after submission.** Once submitted, report the court review outcome. The system will notify the user automatically when the status changes.

Required fields before submitting: title, description, skill_name, at least one tool.
Default values if not discussed: data_sensitivity=1, network_exposure=1, privilege_level=1.

`

	// Query daemon for active skills.
	resp, err := daemonClient.Call(ctx, "skill.list", nil)
	if err == nil && resp.Success && len(resp.Data) > 0 {
		var skills []struct {
			Name      string            `json:"name"`
			State     string            `json:"state"`
			SandboxID string            `json:"sandbox_id"`
			Metadata  map[string]string `json:"metadata,omitempty"`
		}
		if json.Unmarshal(resp.Data, &skills) == nil && len(skills) > 0 {
			base += "Currently registered skills:\n"
			for _, s := range skills {
				base += fmt.Sprintf("  - skill \"%s\" (state: %s) — has tool \"greet\"\n", s.Name, s.State)
			}
			base += "\nExample: to call the greet tool on hello-world, output:\n"
			base += "```tool-call\n{\"skill\": \"hello-world\", \"tool\": \"greet\", \"args\": \"\"}\n```\n"
		}
	}

	return base
}

// parseToolCalls extracts tool-call JSON blocks from LLM output.
// Accepts both ```tool-call and ```json fenced blocks.
func parseToolCalls(content string) []tui.ToolCall {
	var calls []tui.ToolCall
	// Try both fence markers — LLMs sometimes use ```json instead of ```tool-call.
	for _, marker := range []string{"```tool-call", "```json"} {
		search := content
		for {
			start := strings.Index(search, marker)
			if start < 0 {
				break
			}
			after := search[start+len(marker):]
			end := strings.Index(after, "```")
			if end < 0 {
				break
			}
			block := strings.TrimSpace(after[:end])
			var tc struct {
				Skill string          `json:"skill"`
				Tool  string          `json:"tool"`
				Args  json.RawMessage `json:"args"`
			}
			if json.Unmarshal([]byte(block), &tc) == nil && tc.Skill != "" && tc.Tool != "" {
				calls = append(calls, tui.ToolCall{
					Name: tc.Skill + "." + tc.Tool,
					Args: string(tc.Args),
				})
			}
			search = after[end+3:]
		}
	}
	return calls
}

// cleanToolCallContent removes tool-call and json blocks containing skill invocations.
func cleanToolCallContent(content string) string {
	for _, marker := range []string{"```tool-call", "```json"} {
		for {
			start := strings.Index(content, marker)
			if start < 0 {
				break
			}
			after := content[start+len(marker):]
			end := strings.Index(after, "```")
			if end < 0 {
				break
			}
			content = content[:start] + after[end+3:]
		}
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

// --- Proposal tool handlers ---

// handleProposalCreateDraft creates a new draft proposal from LLM-collected fields.
func handleProposalCreateDraft(env *runtimeEnv, argsJSON string) (string, error) {
	var args struct {
		Title           string   `json:"title"`
		Description     string   `json:"description"`
		SkillName       string   `json:"skill_name"`
		Tools           []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
		DataSensitivity int      `json:"data_sensitivity"`
		NetworkExposure int      `json:"network_exposure"`
		PrivilegeLevel  int      `json:"privilege_level"`
		AllowedHosts    []string `json:"allowed_hosts"`
		AllowedPorts    []int    `json:"allowed_ports"`
		SecretRefs      []string `json:"secret_refs"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	if args.Title == "" {
		return "", fmt.Errorf("title is required")
	}
	if args.SkillName == "" {
		return "", fmt.Errorf("skill_name is required")
	}
	if len(args.Tools) == 0 {
		return "", fmt.Errorf("at least one tool is required")
	}

	// Clamp risk dimensions.
	ds := clampInt(args.DataSensitivity, 1, 5)
	ne := clampInt(args.NetworkExposure, 1, 5)
	pl := clampInt(args.PrivilegeLevel, 1, 5)

	// Build wizard result to reuse existing spec generation.
	toolSpecs := make([]wizard.WizardToolSpec, len(args.Tools))
	for i, t := range args.Tools {
		toolSpecs[i] = wizard.WizardToolSpec{Name: t.Name, Description: t.Description}
	}
	ports := make([]uint16, len(args.AllowedPorts))
	for i, p := range args.AllowedPorts {
		if p > 0 && p <= 65535 {
			ports[i] = uint16(p)
		}
	}

	result := &wizard.WizardResult{
		Title:            args.Title,
		Description:      args.Description,
		Category:         "new_skill",
		SkillName:        args.SkillName,
		DataSensitivity:  ds,
		NetworkExposure:  ne,
		PrivilegeLevel:   pl,
		NeedsNetwork:     len(args.AllowedHosts) > 0,
		AllowedHosts:     args.AllowedHosts,
		AllowedPorts:     ports,
		SecretsRefs:      args.SecretRefs,
		RequiredPersonas: []string{"CISO", "SeniorCoder", "SecurityArchitect", "Tester", "UserAdvocate"},
		Tools:            toolSpecs,
	}
	result.Risk = result.ComputedRisk()

	p, err := proposal.NewProposal(result.Title, result.Description, proposal.CategoryNewSkill, "operator")
	if err != nil {
		return "", fmt.Errorf("invalid proposal: %w", err)
	}
	p.Risk = proposal.RiskLevel(result.Risk)
	p.TargetSkill = result.SkillName
	spec, err := result.ToProposalJSON()
	if err != nil {
		return "", fmt.Errorf("spec generation: %w", err)
	}
	p.Spec = spec
	p.SecretsRefs = result.SecretsRefs
	if result.NeedsNetwork {
		p.NetworkPolicy = &proposal.ProposalNetworkPolicy{
			DefaultDeny:  true,
			AllowedHosts: result.AllowedHosts,
			AllowedPorts: ports,
		}
	} else {
		p.NetworkPolicy = &proposal.ProposalNetworkPolicy{DefaultDeny: true}
	}

	if err := env.ProposalStore.Create(p); err != nil {
		return "", fmt.Errorf("failed to save: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"proposal_id": p.ID, "title": p.Title, "skill_name": result.SkillName,
	})
	action := kernel.NewAction(kernel.ActionProposalCreate, "chat", payload)
	env.Kernel.SignAndLog(action)

	return fmt.Sprintf("Draft proposal created.\n  ID: %s\n  Title: %s\n  Skill: %s\n  Risk: %s\n  Status: %s",
		p.ID, p.Title, p.TargetSkill, p.Risk, p.Status), nil
}

// handleProposalUpdateDraft updates fields on an existing draft proposal.
func handleProposalUpdateDraft(env *runtimeEnv, argsJSON string) (string, error) {
	var args struct {
		ID              string   `json:"id"`
		Title           *string  `json:"title"`
		Description     *string  `json:"description"`
		SkillName       *string  `json:"skill_name"`
		Tools           []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
		DataSensitivity *int     `json:"data_sensitivity"`
		NetworkExposure *int     `json:"network_exposure"`
		PrivilegeLevel  *int     `json:"privilege_level"`
		AllowedHosts    []string `json:"allowed_hosts"`
		AllowedPorts    []int    `json:"allowed_ports"`
		SecretRefs      []string `json:"secret_refs"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	p, err := env.ProposalStore.Get(args.ID)
	if err != nil {
		return "", fmt.Errorf("proposal not found: %w", err)
	}
	if p.Status != proposal.StatusDraft {
		return "", fmt.Errorf("can only update draft proposals (current status: %s)", p.Status)
	}

	if args.Title != nil {
		p.Title = *args.Title
	}
	if args.Description != nil {
		p.Description = *args.Description
	}
	if args.SkillName != nil {
		p.TargetSkill = *args.SkillName
	}

	// If tools or risk dimensions provided, regenerate spec.
	if len(args.Tools) > 0 || args.DataSensitivity != nil || args.NetworkExposure != nil || args.PrivilegeLevel != nil {
		// Read current spec to preserve existing values.
		ds, ne, pl := 1, 1, 1
		if args.DataSensitivity != nil {
			ds = clampInt(*args.DataSensitivity, 1, 5)
		}
		if args.NetworkExposure != nil {
			ne = clampInt(*args.NetworkExposure, 1, 5)
		}
		if args.PrivilegeLevel != nil {
			pl = clampInt(*args.PrivilegeLevel, 1, 5)
		}

		toolSpecs := make([]wizard.WizardToolSpec, len(args.Tools))
		for i, t := range args.Tools {
			toolSpecs[i] = wizard.WizardToolSpec{Name: t.Name, Description: t.Description}
		}

		ports := make([]uint16, len(args.AllowedPorts))
		for i, port := range args.AllowedPorts {
			if port > 0 && port <= 65535 {
				ports[i] = uint16(port)
			}
		}

		result := &wizard.WizardResult{
			Title:            p.Title,
			Description:      p.Description,
			Category:         "new_skill",
			SkillName:        p.TargetSkill,
			DataSensitivity:  ds,
			NetworkExposure:  ne,
			PrivilegeLevel:   pl,
			NeedsNetwork:     len(args.AllowedHosts) > 0,
			AllowedHosts:     args.AllowedHosts,
			AllowedPorts:     ports,
			SecretsRefs:      args.SecretRefs,
			RequiredPersonas: []string{"CISO", "SeniorCoder", "SecurityArchitect", "Tester", "UserAdvocate"},
			Tools:            toolSpecs,
		}
		result.Risk = result.ComputedRisk()
		p.Risk = proposal.RiskLevel(result.Risk)

		spec, err := result.ToProposalJSON()
		if err != nil {
			return "", fmt.Errorf("spec generation: %w", err)
		}
		p.Spec = spec

		if result.NeedsNetwork {
			p.NetworkPolicy = &proposal.ProposalNetworkPolicy{
				DefaultDeny:  true,
				AllowedHosts: result.AllowedHosts,
				AllowedPorts: ports,
			}
		}
	}

	if args.SecretRefs != nil {
		p.SecretsRefs = args.SecretRefs
	}

	if err := env.ProposalStore.Update(p); err != nil {
		return "", fmt.Errorf("failed to save: %w", err)
	}

	return fmt.Sprintf("Draft updated.\n  ID: %s\n  Title: %s\n  Skill: %s\n  Risk: %s",
		p.ID, p.Title, p.TargetSkill, p.Risk), nil
}

// handleProposalGetDraft loads and returns a proposal's details.
func handleProposalGetDraft(env *runtimeEnv, argsJSON string) (string, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	p, err := env.ProposalStore.Get(args.ID)
	if err != nil {
		return "", fmt.Errorf("not found: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Proposal: %s\n", p.ID)
	fmt.Fprintf(&b, "  Title:       %s\n", p.Title)
	fmt.Fprintf(&b, "  Description: %s\n", p.Description)
	fmt.Fprintf(&b, "  Skill:       %s\n", p.TargetSkill)
	fmt.Fprintf(&b, "  Category:    %s\n", p.Category)
	fmt.Fprintf(&b, "  Status:      %s\n", p.Status)
	fmt.Fprintf(&b, "  Risk:        %s\n", p.Risk)
	fmt.Fprintf(&b, "  Round:       %d\n", p.Round)
	fmt.Fprintf(&b, "  Version:     %d\n", p.Version)
	if len(p.SecretsRefs) > 0 {
		fmt.Fprintf(&b, "  Secrets:     %v\n", p.SecretsRefs)
	}
	if p.NetworkPolicy != nil && len(p.NetworkPolicy.AllowedHosts) > 0 {
		fmt.Fprintf(&b, "  Network:     %v\n", p.NetworkPolicy.AllowedHosts)
	}
	if len(p.Spec) > 0 {
		fmt.Fprintf(&b, "  Spec:\n%s\n", string(p.Spec))
	}
	if len(p.Reviews) > 0 {
		fmt.Fprintf(&b, "  Reviews:\n")
		for _, r := range p.Reviews {
			fmt.Fprintf(&b, "    %s: %s (risk=%.1f) %s\n", r.Persona, r.Verdict, r.RiskScore, r.Comments)
		}
	}
	if len(p.History) > 0 {
		fmt.Fprintf(&b, "  History:\n")
		for _, h := range p.History {
			fmt.Fprintf(&b, "    %s → %s by %s: %s\n", h.From, h.To, h.Actor, h.Reason)
		}
	}
	return b.String(), nil
}

// handleProposalListDrafts returns all proposals, showing drafts prominently.
func handleProposalListDrafts(env *runtimeEnv) (string, error) {
	summaries, err := env.ProposalStore.List()
	if err != nil {
		return "", err
	}
	if len(summaries) == 0 {
		return "No proposals found. Start by describing a skill you'd like to create.", nil
	}
	var lines []string
	for _, s := range summaries {
		lines = append(lines, fmt.Sprintf("  %s  %-28s  [%s]  risk=%s  round=%d",
			s.ID[:8], truncate(s.Title, 28), s.Status, s.Risk, s.Round))
	}
	return "Proposals:\n" + strings.Join(lines, "\n"), nil
}

// handleProposalSubmit transitions a draft to submitted and optionally starts court review.
func handleProposalSubmit(env *runtimeEnv, daemonClient *api.Client, ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	p, err := env.ProposalStore.Get(args.ID)
	if err != nil {
		return "", fmt.Errorf("not found: %w", err)
	}
	if p.Status != proposal.StatusDraft {
		return fmt.Sprintf("Proposal is already %s (not draft).", p.Status), nil
	}

	if err := p.Transition(proposal.StatusSubmitted, "submitted for court review", "operator"); err != nil {
		return "", fmt.Errorf("transition failed: %w", err)
	}
	if err := env.ProposalStore.Update(p); err != nil {
		return "", fmt.Errorf("failed to save: %w", err)
	}

	payload, _ := json.Marshal(map[string]string{"proposal_id": p.ID})
	action := kernel.NewAction(kernel.ActionProposalSubmit, "chat", payload)
	env.Kernel.SignAndLog(action)

	result := fmt.Sprintf("Proposal submitted for court review.\n  ID: %s\n  Status: %s", p.ID, p.Status)

	// Verify the submission was persisted.
	verified, verifyErr := env.ProposalStore.Get(p.ID)
	if verifyErr != nil {
		result += fmt.Sprintf("\n\nWarning: could not verify submission: %v", verifyErr)
	} else if verified.Status == proposal.StatusDraft {
		result += "\n\nWarning: proposal is still in draft status — submission may have failed."
	}

	// Try to start court review via daemon.
	pData, _ := p.Marshal()
	resp, err := daemonClient.Call(ctx, "court.review", api.CourtReviewRequest{
		ProposalID:   p.ID,
		ProposalData: pData,
	})
	if err == nil && resp.Success {
		// Parse court session result.
		var session struct {
			State   string  `json:"state"`
			Verdict string  `json:"verdict"`
			Risk    float64 `json:"risk"`
		}
		if json.Unmarshal(resp.Data, &session) == nil {
			result += fmt.Sprintf("\n\nCourt review completed.\n  State: %s\n  Verdict: %s\n  Risk: %.1f",
				session.State, session.Verdict, session.Risk)
		}
	} else if err != nil {
		result += fmt.Sprintf("\n\nCourt review could not be started automatically: %v\nRun manually: aegisclaw court review %s", err, p.ID)
	}

	return result, nil
}

// handleProposalStatus checks the current status of a proposal.
func handleProposalStatus(env *runtimeEnv, argsJSON string) (string, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	p, err := env.ProposalStore.Get(args.ID)
	if err != nil {
		return "", fmt.Errorf("not found: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Proposal %s status: %s\n", p.ID[:8], p.Status)
	fmt.Fprintf(&b, "  Title: %s\n", p.Title)
	fmt.Fprintf(&b, "  Risk: %s\n", p.Risk)
	fmt.Fprintf(&b, "  Round: %d\n", p.Round)
	if len(p.Reviews) > 0 {
		fmt.Fprintf(&b, "  Latest reviews:\n")
		for _, r := range p.ReviewsForRound(p.Round) {
			fmt.Fprintf(&b, "    %s: %s (risk=%.1f)\n", r.Persona, r.Verdict, r.RiskScore)
		}
	}
	if len(p.History) > 0 {
		last := p.History[len(p.History)-1]
		fmt.Fprintf(&b, "  Last change: %s → %s (%s)\n", last.From, last.To, last.Reason)
	}
	return b.String(), nil
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
