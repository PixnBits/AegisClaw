package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/audit"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/tui"
	"github.com/PixnBits/AegisClaw/internal/wizard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Interactive ReAct chat interface",
	Long: `Opens an interactive chat interface with persistent context.
Supports slash commands for quick access to AegisClaw operations:
  /help          - Show available slash commands
  /call          - Invoke a skill tool: /call <skill>.<tool> [args...]
  /status        - Show system status (sandboxes, skills, audit)
  /audit         - Show audit chain info and verification
  /safe-mode     - Stop all skills and block execution (no LLM)
  /safe-mode off - Re-enable skill execution
  /shutdown      - Emergency stop: all skills + daemon + exit
  /quit          - Exit chat
  /exit          - Exit chat`,
	RunE: runChat,
}

func runChat(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	// Open a per-session audit log.
	sessionLog, err := audit.NewSessionLog(env.Config.Audit.Dir)
	if err != nil {
		env.Logger.Warn("failed to create chat session log", zap.Error(err))
		// Non-fatal: continue without session logging.
	} else {
		defer sessionLog.Close()
	}

	model := tui.NewChatModel()

	// D2: All LLM interaction is routed through the daemon API.
	// The CLI process is a thin TUI client — it never calls Ollama directly.
	daemonClient := api.NewClient(env.Config.Daemon.SocketPath)

	model.SendMessage = func(input string, history []tui.ChatMessage) (tui.ChatMessage, []tui.ToolCall, error) {
		// Handle slash commands via daemon.
		if strings.HasPrefix(input, "/") {
			if sessionLog != nil {
				sessionLog.Log(audit.SessionEvent{Event: audit.EventSlashCommand, Role: "user", Content: input})
			}

			// /quit and /safe-mode are handled locally for responsiveness.
			parts := strings.Fields(input)
			switch parts[0] {
			case "/quit":
				return tui.ChatMessage{
					Role:      tui.ChatRoleAssistant,
					Content:   "Goodbye!",
					Timestamp: time.Now(),
				}, nil, nil
			case "/safe-mode":
				enable := true
				if len(parts) > 1 && parts[1] == "off" {
					enable = false
				}
				action := "safe-mode.enable"
				if !enable {
					action = "safe-mode.disable"
				}
				resp, daemonErr := daemonClient.Call(cmd.Context(), action, nil)
				if daemonErr != nil {
					return tui.ChatMessage{}, nil, fmt.Errorf("daemon: %w", daemonErr)
				}
				msg := "Safe mode enabled. All skills deactivated."
				if !enable {
					msg = "Safe mode disabled. Skill operations re-enabled."
				}
				if !resp.Success {
					msg = "Failed: " + resp.Error
				}
				return tui.ChatMessage{
					Role:      tui.ChatRoleAssistant,
					Content:   msg,
					Timestamp: time.Now(),
				}, nil, nil
			case "/shutdown":
				if resp, shutErr := daemonClient.Call(cmd.Context(), "safe-mode.enable", nil); shutErr != nil {
					return tui.ChatMessage{}, nil, fmt.Errorf("safe-mode: %w", shutErr)
				} else if !resp.Success {
					return tui.ChatMessage{}, nil, fmt.Errorf("safe-mode: %s", resp.Error)
				}
				if resp, shutErr := daemonClient.Call(cmd.Context(), "kernel.shutdown", nil); shutErr != nil {
					return tui.ChatMessage{}, nil, fmt.Errorf("shutdown: %w", shutErr)
				} else if !resp.Success {
					return tui.ChatMessage{}, nil, fmt.Errorf("shutdown: %s", resp.Error)
				}
				return tui.ChatMessage{
					Role:      tui.ChatRoleAssistant,
					Content:   "Daemon shutdown initiated.",
					Timestamp: time.Now(),
				}, nil, nil
			}

			// Route all other slash commands to daemon.
			resp, err := daemonClient.Call(cmd.Context(), "chat.slash", api.ChatSlashRequest{
				Command: input,
			})
			if err != nil {
				return tui.ChatMessage{}, nil, fmt.Errorf("daemon: %w", err)
			}
			if !resp.Success {
				return tui.ChatMessage{}, nil, fmt.Errorf("daemon: %s", resp.Error)
			}
			var chatResp api.ChatMessageResponse
			if unmarshalErr := json.Unmarshal(resp.Data, &chatResp); unmarshalErr != nil {
				return tui.ChatMessage{}, nil, fmt.Errorf("parse response: %w", unmarshalErr)
			}
			if sessionLog != nil {
				sessionLog.Log(audit.SessionEvent{Event: audit.EventAssistantMessage, Role: "assistant", Content: chatResp.Content})
			}
			return tui.ChatMessage{
				Role:      tui.ChatRoleAssistant,
				Content:   chatResp.Content,
				Timestamp: time.Now(),
			}, nil, nil
		}

		// D2: Route regular messages through the daemon, which handles LLM interaction.
		historyItems := make([]api.ChatHistoryItem, 0, len(history))
		for _, h := range history {
			if h.Role == tui.ChatRoleSystem {
				continue
			}
			role := "user"
			if h.Role == tui.ChatRoleAssistant {
				role = "assistant"
			}
			content := h.Content
			// Use OriginalContent (which includes tool-call blocks) so the
			// LLM sees its own prior tool calls and understands the pattern.
			if h.Role == tui.ChatRoleAssistant && h.OriginalContent != "" {
				content = h.OriginalContent
			}
			// Tag tool results so the LLM distinguishes them from user input.
			if h.Role == tui.ChatRoleTool {
				content = fmt.Sprintf("[Tool %q returned]:\n%s", h.ToolName, h.Content)
			}
			historyItems = append(historyItems, api.ChatHistoryItem{Role: role, Content: content})
		}

		if sessionLog != nil {
			sessionLog.Log(audit.SessionEvent{Event: audit.EventUserMessage, Role: "user", Content: input})
		}

		resp, err := daemonClient.Call(cmd.Context(), "chat.message", api.ChatMessageRequest{
			Input:   input,
			History: historyItems,
		})
		if err != nil {
			return tui.ChatMessage{}, nil, fmt.Errorf("daemon: %w", err)
		}
		if !resp.Success {
			return tui.ChatMessage{}, nil, fmt.Errorf("daemon: %s", resp.Error)
		}

		var chatResp api.ChatMessageResponse
		if unmarshalErr := json.Unmarshal(resp.Data, &chatResp); unmarshalErr != nil {
			return tui.ChatMessage{}, nil, fmt.Errorf("parse response: %w", unmarshalErr)
		}

		content := chatResp.Content

		// Check if the LLM wants to invoke a skill tool.
		toolCalls := parseToolCalls(content)
		if len(toolCalls) > 0 {
			cleaned := cleanToolCallContent(content)
			// When the LLM only emitted a tool-call block (no surrounding prose),
			// show a friendly interim message so the user knows what's happening.
			if cleaned == "" {
				cleaned = toolCallFriendlyLabel(toolCalls[0].Name)
			}
			if sessionLog != nil {
				sessionLog.Log(audit.SessionEvent{Event: audit.EventAssistantMessage, Role: "assistant", Content: cleaned})
			}
			return tui.ChatMessage{
				Role:            tui.ChatRoleAssistant,
				Content:         cleaned,
				Timestamp:       time.Now(),
				OriginalContent: content, // preserve tool-call block for history
			}, toolCalls, nil
		}

		if sessionLog != nil {
			sessionLog.Log(audit.SessionEvent{Event: audit.EventAssistantMessage, Role: "assistant", Content: content})
		}
		return tui.ChatMessage{
			Role:      tui.ChatRoleAssistant,
			Content:   content,
			Timestamp: time.Now(),
		}, nil, nil
	}

	model.ExecuteTool = func(call tui.ToolCall) (string, error) {
		if sessionLog != nil {
			sessionLog.Log(audit.SessionEvent{Event: audit.EventToolCall, ToolName: call.Name, ToolArgs: call.Args})
		}
		var result string
		var toolErr error
		defer func() {
			if sessionLog != nil {
				evt := audit.SessionEvent{Event: audit.EventToolResult, ToolName: call.Name, Content: result}
				if toolErr != nil {
					evt.Error = toolErr.Error()
				}
				sessionLog.Log(evt)
			}
		}()

		// Proposal tools are handled locally because they need the
		// proposal store and are latency-sensitive for the wizard flow.
		switch call.Name {
		case "proposal.create_draft":
			result, toolErr = handleProposalCreateDraft(env, cmd.Context(), call.Args)
			return result, toolErr
		case "proposal.update_draft":
			result, toolErr = handleProposalUpdateDraft(env, cmd.Context(), call.Args)
			return result, toolErr
		case "proposal.get_draft":
			result, toolErr = handleProposalGetDraft(env, cmd.Context(), call.Args)
			return result, toolErr
		case "proposal.list_drafts":
			result, toolErr = handleProposalListDrafts(env, cmd.Context())
			return result, toolErr
		case "proposal.submit":
			result, toolErr = handleProposalSubmit(env, daemonClient, cmd.Context(), call.Args)
			return result, toolErr
		case "proposal.status":
			result, toolErr = handleProposalStatus(env, cmd.Context(), call.Args)
			return result, toolErr
		case "proposal.reviews":
			result, toolErr = handleProposalReviews(env, cmd.Context(), call.Args)
			return result, toolErr
		case "proposal.vote":
			result, toolErr = handleProposalVote(env, cmd.Context(), call.Args)
			return result, toolErr
		}

		// D2: Route all other tool calls through the daemon.
		resp, err := daemonClient.Call(cmd.Context(), "chat.tool", api.ChatToolExecRequest{
			Name: call.Name,
			Args: call.Args,
		})
		if err != nil {
			toolErr = fmt.Errorf("daemon: %w", err)
			return "", toolErr
		}
		if !resp.Success {
			toolErr = fmt.Errorf("tool failed: %s", resp.Error)
			return "", toolErr
		}
		var toolResp struct {
			Result string `json:"result"`
		}
		if unmarshalErr := json.Unmarshal(resp.Data, &toolResp); unmarshalErr != nil {
			result = string(resp.Data)
		} else {
			result = toolResp.Result
		}
		return result, nil
	}

	model.SummarizeToolResult = func(toolName, toolResult string, history []tui.ChatMessage) (tui.ChatMessage, error) {
		// D2: Route summarization through the daemon.
		historyItems := make([]api.ChatHistoryItem, 0, len(history))
		for _, h := range history {
			if h.Role == tui.ChatRoleSystem {
				continue
			}
			role := "user"
			if h.Role == tui.ChatRoleAssistant {
				role = "assistant"
			}
			content := h.Content
			if h.Role == tui.ChatRoleAssistant && h.OriginalContent != "" {
				content = h.OriginalContent
			}
			if h.Role == tui.ChatRoleTool {
				content = fmt.Sprintf("[Tool %q returned]:\n%s", h.ToolName, h.Content)
			}
			historyItems = append(historyItems, api.ChatHistoryItem{Role: role, Content: content})
		}

		resp, err := daemonClient.Call(cmd.Context(), "chat.summarize", api.ChatSummarizeRequest{
			ToolName:   toolName,
			ToolResult: toolResult,
			History:    historyItems,
		})
		if err != nil {
			return tui.ChatMessage{}, fmt.Errorf("daemon: %w", err)
		}
		if !resp.Success {
			return tui.ChatMessage{}, fmt.Errorf("daemon: %s", resp.Error)
		}

		var chatResp api.ChatMessageResponse
		if unmarshalErr := json.Unmarshal(resp.Data, &chatResp); unmarshalErr != nil {
			return tui.ChatMessage{}, fmt.Errorf("parse response: %w", unmarshalErr)
		}

		if sessionLog != nil {
			sessionLog.Log(audit.SessionEvent{Event: audit.EventAssistantMessage, Role: "assistant", Content: chatResp.Content, ToolName: toolName})
		}
		return tui.ChatMessage{
			Role:      tui.ChatRoleAssistant,
			Content:   chatResp.Content,
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

// buildSystemPrompt and handleSlashCommand have been moved to the daemon side
// (chat_handlers.go) as part of D2. The CLI is now a thin TUI client.

// buildSystemPrompt constructs the LLM system prompt including available skills.
// Retained for backward compatibility with tests; the daemon uses buildDaemonSystemPrompt.
func buildSystemPrompt(ctx context.Context, daemonClient *api.Client) string {
	base := `You are AegisClaw, an AI-powered software governance assistant.

You help users manage skills (sandboxed microVM workloads), proposals, and system operations.

## Slash commands (handled locally, not by you)
  /help       - Show available commands
  /call       - Invoke a skill tool: /call <skill>.<tool> [args...]
  /status     - System status (sandboxes, skills, audit)
  /audit      - Audit chain info and verification
  /safe-mode  - Stop all skills and block execution (no LLM)
  /safe-mode off - Re-enable skill execution
  /shutdown   - Emergency daemon shutdown
  /quit       - Exit chat
  /exit       - Exit chat

## Tool invocation format
To call a tool, output EXACTLY one tool-call block per message:
` + "```tool-call" + `
{"skill": "<namespace>", "tool": "<tool-name>", "args": <json-object>}
` + "```" + `

DO: Output the tool-call block and wait for the result.
DO NOT: Describe what you would do, show example JSON, or make up IDs. ACT by outputting a tool-call block.

## Proposal tools (namespace: "proposal")
All proposal tools use "skill": "proposal". The tool names are:
- create_draft — args: {"title": "...", "description": "...", "skill_name": "...", "tools": [{"name": "...", "description": "..."}], "data_sensitivity": 1-5, "network_exposure": 1-5, "privilege_level": 1-5, "allowed_hosts": [], "allowed_ports": [], "egress_mode": "proxy|direct", "secret_refs": [], "capabilities": {"network": true, "secrets": [], "filesystem_write": false}}
- update_draft — args: {"id": "<uuid>", ...fields to change (same fields as create_draft plus egress_mode, capabilities)}
- get_draft — args: {"id": "<uuid>"}
- list_drafts — args: {}
- submit — args: {"id": "<uuid>"}
- status — args: {"id": "<uuid>"}

Defaults if not discussed: data_sensitivity=1, network_exposure=1, privilege_level=1, egress_mode="proxy" (when network is needed).
Required before submit: title, description, skill_name, at least one tool.
For network skills: include allowed_hosts (exact FQDNs only, no wildcards), allowed_ports (usually [443]), egress_mode="proxy", capabilities.network=true.
For secret-using skills: include secret_refs and capabilities.secrets with the same names. Secrets are added separately via CLI.

## How to handle a skill proposal request

When a user asks you to create or propose a skill:
1. Infer the fields from their description. For simple skills, fill in sensible defaults.
2. Immediately output a tool-call block to call create_draft. Do NOT just describe the proposal — actually call the tool.
3. After the system returns the draft (with its real UUID), present it to the user and ask them to confirm.
4. When the user confirms, output a tool-call block to call submit using the EXACT UUID from step 3.

Example — user says "propose a hello skill":
` + "```tool-call" + `
{"skill": "proposal", "tool": "create_draft", "args": {"title": "Hello Skill", "description": "A simple greeting skill", "skill_name": "hello", "tools": [{"name": "greet", "description": "Say hello"}]}}
` + "```" + `
Then after the system returns ID 550e8400-e29b-41d4-a716-446655440000, and the user confirms:
` + "```tool-call" + `
{"skill": "proposal", "tool": "submit", "args": {"id": "550e8400-e29b-41d4-a716-446655440000"}}
` + "```" + `

CRITICAL RULES:
- Output ONLY ONE tool-call block per message. Wait for the result before the next call.
- The "skill" field for ALL proposal tools is always "proposal". Never use the skill being proposed as the namespace.
- Never invent or guess proposal IDs. Only use IDs returned by the system.
- Always show the full UUID to the user after creating or submitting a proposal.

## Skill tools
For active skills (listed below), use the skill's own name as the namespace:
` + "```tool-call" + `
{"skill": "hello-world", "tool": "greet", "args": ""}
` + "```" + `

Users can also invoke skills directly with: /call <skill>.<tool> [args...]
Example: /call hello-world.greet "world"

`

	// Query daemon for active skills.
	if daemonClient == nil {
		return base
	}
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
// Accepts ```tool-call, ```json, and plain ``` fenced blocks (small models
// often omit the language tag). Returns at most ONE tool call to prevent
// the LLM from chaining calls with stale/guessed IDs.
func parseToolCalls(content string) []tui.ToolCall {
	// proposalTools are tool names that belong under the "proposal." namespace.
	// If an LLM uses the wrong skill namespace for these, we auto-correct.
	proposalTools := map[string]bool{
		"create_draft": true, "update_draft": true,
		"get_draft": true, "list_drafts": true,
		"submit": true, "status": true,
		"reviews": true, "vote": true,
	}

	// Try fence markers in priority order. Plain "```" is last because it also
	// matches the prefix of the tagged variants; the JSON unmarshal check
	// rejects blocks with a language tag prefix (e.g. "tool-call\n{...}").
	for _, marker := range []string{"```tool-call", "```json", "```"} {
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

			// Primary: {"name": "tool_name", "args": {...}}
			var tc struct {
				Name string          `json:"name"`
				Args json.RawMessage `json:"args"`
			}
			if json.Unmarshal([]byte(block), &tc) == nil && tc.Name != "" {
				return []tui.ToolCall{{
					Name: tc.Name,
					Args: string(tc.Args),
				}}
			}

			// Legacy: {"skill": "proposal", "tool": "create_draft", "args": {...}}
			var legacy struct {
				Skill string          `json:"skill"`
				Tool  string          `json:"tool"`
				Args  json.RawMessage `json:"args"`
			}
			if json.Unmarshal([]byte(block), &legacy) == nil && legacy.Tool != "" {
				name := legacy.Skill + "." + legacy.Tool
				// Auto-correct namespace: if the tool is a proposal tool
				// but the skill isn't "proposal", fix it.
				if proposalTools[legacy.Tool] && legacy.Skill != "proposal" {
					name = "proposal." + legacy.Tool
				}
				return []tui.ToolCall{{
					Name: name,
					Args: string(legacy.Args),
				}}
			}

			search = after[end+3:]
		}
	}

	// Fallback: try to find bare JSON {"name": "..."} outside of fences.
	// Small models sometimes omit the fence wrapper entirely.
	// Only match top-level objects that have both "name" and structurally
	// look like a tool call (not nested objects inside arrays).
	if idx := strings.Index(content, `{"name"`); idx >= 0 {
		// Skip if this position is inside a fenced block.
		beforeIdx := content[:idx]
		fenceCount := strings.Count(beforeIdx, "```")
		if fenceCount%2 == 0 { // even = not inside a fence
			rest := content[idx:]
			depth, end := 0, -1
			for i, ch := range rest {
				switch ch {
				case '{':
					depth++
				case '}':
					depth--
					if depth == 0 {
						end = i + 1
					}
				}
				if end >= 0 {
					break
				}
			}
			if end > 0 {
				var tc struct {
					Name string          `json:"name"`
					Args json.RawMessage `json:"args"`
				}
				if json.Unmarshal([]byte(rest[:end]), &tc) == nil && tc.Name != "" {
					return []tui.ToolCall{{
						Name: tc.Name,
						Args: string(tc.Args),
					}}
				}
			}
		}
	}

	return nil
}

// cleanToolCallContent removes tool-call blocks and any post-tool-call text.
// When an LLM emits a tool call, everything after the first block is typically
// hallucinated narration ("Running the tool..." + fabricated results), so we
// truncate there and keep only the pre-tool-call prose.
func cleanToolCallContent(content string) string {
	// Find the earliest tool-call block across all marker types.
	firstToolPos := -1

	// Check tagged and plain fences.
	for _, marker := range []string{"```tool-call", "```json", "```"} {
		search := content
		offset := 0
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

			// For plain ``` fences, only count blocks that contain tool-call JSON.
			if marker == "```" {
				var tc struct {
					Name string `json:"name"`
				}
				var legacy struct {
					Tool string `json:"tool"`
				}
				isToolCall := (json.Unmarshal([]byte(block), &tc) == nil && tc.Name != "") ||
					(json.Unmarshal([]byte(block), &legacy) == nil && legacy.Tool != "")
				if !isToolCall {
					search = after[end+3:]
					offset += start + len(marker) + end + 3
					continue
				}
			}

			pos := offset + start
			if firstToolPos < 0 || pos < firstToolPos {
				firstToolPos = pos
			}
			break // only need the first occurrence per marker
		}
	}

	// Check bare JSON tool-call objects outside fences.
	if idx := strings.Index(content, `{"name"`); idx >= 0 {
		beforeIdx := content[:idx]
		fenceCount := strings.Count(beforeIdx, "```")
		if fenceCount%2 == 0 { // not inside a fence
			rest := content[idx:]
			depth, end := 0, -1
			for i, ch := range rest {
				switch ch {
				case '{':
					depth++
				case '}':
					depth--
					if depth == 0 {
						end = i + 1
					}
				}
				if end >= 0 {
					break
				}
			}
			if end > 0 {
				var tc struct {
					Name string `json:"name"`
				}
				if json.Unmarshal([]byte(rest[:end]), &tc) == nil && tc.Name != "" {
					if firstToolPos < 0 || idx < firstToolPos {
						firstToolPos = idx
					}
				}
			}
		}
	}

	// Truncate at the first tool-call position — everything after is likely
	// hallucinated ("Running the tool..." + fabricated results).
	if firstToolPos >= 0 {
		content = content[:firstToolPos]
	}

	// Strip bare "tool-call" / "tool_call" labels left behind by small models
	// that emit the fence language tag without the triple-backtick fences.
	content = strings.TrimSpace(content)
	for _, label := range []string{"tool-call", "tool_call"} {
		content = strings.ReplaceAll(content, label, "")
	}
	return strings.TrimSpace(content)
}

// toolCallFriendlyLabel returns a user-friendly description like "Checking proposals..."
// for a given tool name. Used as the interim message while a tool is executing.
func toolCallFriendlyLabel(name string) string {
	switch name {
	case "list_skills":
		return "Looking up skills…"
	case "list_proposals", "proposal.list_drafts":
		return "Checking proposals…"
	case "list_sandboxes":
		return "Listing sandboxes…"
	case "proposal.create_draft":
		return "Creating a proposal draft…"
	case "proposal.update_draft":
		return "Updating the proposal…"
	case "proposal.get_draft":
		return "Fetching proposal details…"
	case "proposal.submit":
		return "Submitting proposal for review…"
	case "proposal.status":
		return "Checking proposal status…"
	case "activate_skill":
		return "Activating skill…"
	default:
		if strings.Contains(name, ".") {
			parts := strings.SplitN(name, ".", 2)
			return fmt.Sprintf("Calling %s on %s…", parts[1], parts[0])
		}
		return fmt.Sprintf("Running %s…", name)
	}
}

// --- Proposal tool handlers ---

// resolveProposalID expands a prefix (or full UUID) to the full proposal ID.
func resolveProposalID(env *runtimeEnv, idOrPrefix string) (string, error) {
	return env.ProposalStore.ResolveID(idOrPrefix)
}

// resolveEgressMode returns the egress mode to use for a network policy.
// The precedence is: explicit override > existing value > "proxy" (safe default).
// Pass existingMode="" and override=nil when creating a fresh policy.
func resolveEgressMode(existingMode string, override *string) string {
	if override != nil {
		return *override
	}
	if existingMode != "" {
		return existingMode
	}
	return "proxy"
}

type draftNetworkPolicyArgs struct {
	DefaultDeny      *bool    `json:"default_deny"`
	AllowedHosts     []string `json:"allowed_hosts"`
	AllowedPorts     []int    `json:"allowed_ports"`
	AllowedProtocols []string `json:"allowed_protocols"`
	EgressMode       string   `json:"egress_mode"`
}

type createDraftArgs struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	SkillName   string `json:"skill_name"`
	Tools       []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"tools"`
	DataSensitivity int                         `json:"data_sensitivity"`
	NetworkExposure int                         `json:"network_exposure"`
	PrivilegeLevel  int                         `json:"privilege_level"`
	AllowedHosts    []string                    `json:"allowed_hosts"`
	AllowedPorts    []int                       `json:"allowed_ports"`
	SecretRefs      []string                    `json:"secret_refs"`
	EgressMode      string                      `json:"egress_mode"`
	Capabilities    *proposal.SkillCapabilities `json:"capabilities"`
	NetworkPolicy   *draftNetworkPolicyArgs     `json:"network_policy"`
}

func normalizeAndDedupeHosts(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, host := range in {
		h := strings.ToLower(strings.TrimSpace(host))
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}

func dedupePorts(in []int) []int {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, p := range in {
		if p <= 0 || p > 65535 {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		v := strings.TrimSpace(s)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// handleProposalCreateDraft creates a new draft proposal from LLM-collected fields.
func handleProposalCreateDraft(env *runtimeEnv, ctx context.Context, argsJSON string) (string, error) {
	var args createDraftArgs
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

	allowedHosts := normalizeAndDedupeHosts(args.AllowedHosts)
	allowedPorts := dedupePorts(args.AllowedPorts)
	allowedProtocols := []string(nil)
	secretRefs := dedupeStrings(args.SecretRefs)
	if args.NetworkPolicy != nil {
		if args.NetworkPolicy.DefaultDeny != nil && !*args.NetworkPolicy.DefaultDeny {
			return "", fmt.Errorf("network_policy.default_deny must be true")
		}
		if len(args.NetworkPolicy.AllowedHosts) > 0 {
			allowedHosts = normalizeAndDedupeHosts(args.NetworkPolicy.AllowedHosts)
		}
		if len(args.NetworkPolicy.AllowedPorts) > 0 {
			allowedPorts = dedupePorts(args.NetworkPolicy.AllowedPorts)
		}
		allowedProtocols = dedupeStrings(args.NetworkPolicy.AllowedProtocols)
		if args.NetworkPolicy.EgressMode != "" {
			args.EgressMode = args.NetworkPolicy.EgressMode
		}
	}

	// Build wizard result to reuse existing spec generation.
	toolSpecs := make([]wizard.WizardToolSpec, len(args.Tools))
	for i, t := range args.Tools {
		toolSpecs[i] = wizard.WizardToolSpec{Name: t.Name, Description: t.Description}
	}
	ports := make([]uint16, len(allowedPorts))
	for i, p := range allowedPorts {
		if p > 0 && p <= 65535 {
			ports[i] = uint16(p)
		}
	}
	if args.Capabilities != nil && len(args.Capabilities.Secrets) > 0 {
		secretRefs = dedupeStrings(append(secretRefs, args.Capabilities.Secrets...))
	}

	needsNetwork := len(allowedHosts) > 0 || args.NetworkPolicy != nil || (args.Capabilities != nil && args.Capabilities.Network)
	// Resolve egress mode: default to "proxy" for new network skills.
	var egressOverride *string
	if args.EgressMode != "" {
		egressOverride = &args.EgressMode
	}
	egressMode := ""
	if needsNetwork {
		egressMode = resolveEgressMode("", egressOverride)
	}

	result := &wizard.WizardResult{
		Title:            args.Title,
		Description:      args.Description,
		Category:         "new_skill",
		SkillName:        args.SkillName,
		DataSensitivity:  ds,
		NetworkExposure:  ne,
		PrivilegeLevel:   pl,
		NeedsNetwork:     needsNetwork,
		AllowedHosts:     allowedHosts,
		AllowedPorts:     ports,
		SecretsRefs:      secretRefs,
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

	// Populate NetworkPolicy with explicit egress mode.
	if needsNetwork {
		p.NetworkPolicy = &proposal.ProposalNetworkPolicy{
			DefaultDeny:      true,
			AllowedHosts:     result.AllowedHosts,
			AllowedPorts:     ports,
			AllowedProtocols: allowedProtocols,
			EgressMode:       egressMode,
		}
	} else {
		p.NetworkPolicy = &proposal.ProposalNetworkPolicy{DefaultDeny: true}
	}

	// Populate Capabilities, merging explicit args with inferred values.
	caps := args.Capabilities
	if caps == nil {
		caps = &proposal.SkillCapabilities{}
	}
	if needsNetwork {
		caps.Network = true
	}
	if len(secretRefs) > 0 {
		caps.Secrets = secretRefs
	}
	p.Capabilities = caps

	if err := env.ProposalStore.Create(p); err != nil {
		return "", fmt.Errorf("failed to save: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"proposal_id": p.ID, "title": p.Title, "skill_name": result.SkillName,
		"trace_id": reActTraceIDFromContext(ctx),
	})
	action := kernel.NewAction(kernel.ActionProposalCreate, "chat", payload)
	env.Kernel.SignAndLog(action)

	result_msg := fmt.Sprintf("Draft proposal created.\n  ID: %s\n  Title: %s\n  Skill: %s\n  Risk: %s\n  Status: %s",
		p.ID, p.Title, p.TargetSkill, p.Risk, p.Status)

	// Surface secrets guidance immediately so the user knows what to do
	// before activating the skill (spec §3.3 — pre-activation verification).
	if len(secretRefs) > 0 {
		result_msg += "\n\nRequired secrets (add before activating):"
		for _, ref := range secretRefs {
			result_msg += fmt.Sprintf("\n  aegisclaw secrets add %s --skill %s", ref, args.SkillName)
		}
	}

	return result_msg, nil
}

// handleProposalUpdateDraft updates fields on an existing draft proposal.
func handleProposalUpdateDraft(env *runtimeEnv, ctx context.Context, argsJSON string) (string, error) {
	// Use json.RawMessage for array fields so we can apply lenient
	// coercion for small LLMs that serialize arrays as strings.
	var raw struct {
		ID          string  `json:"id"`
		Title       *string `json:"title"`
		Description *string `json:"description"`
		SkillName   *string `json:"skill_name"`
		Tools       []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
		DataSensitivity *int                        `json:"data_sensitivity"`
		NetworkExposure *int                        `json:"network_exposure"`
		PrivilegeLevel  *int                        `json:"privilege_level"`
		AllowedHosts    json.RawMessage             `json:"allowed_hosts"`
		AllowedPorts    json.RawMessage             `json:"allowed_ports"`
		SecretRefs      json.RawMessage             `json:"secret_refs"`
		EgressMode      *string                     `json:"egress_mode"`
		Capabilities    *proposal.SkillCapabilities `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}

	type toolSpec struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	args := struct {
		ID              string
		Title           *string
		Description     *string
		SkillName       *string
		Tools           []toolSpec
		DataSensitivity *int
		NetworkExposure *int
		PrivilegeLevel  *int
		AllowedHosts    []string
		AllowedPorts    []int
		SecretRefs      []string
		EgressMode      *string
		Capabilities    *proposal.SkillCapabilities
	}{
		ID:              raw.ID,
		Title:           raw.Title,
		Description:     raw.Description,
		SkillName:       raw.SkillName,
		DataSensitivity: raw.DataSensitivity,
		NetworkExposure: raw.NetworkExposure,
		PrivilegeLevel:  raw.PrivilegeLevel,
		AllowedHosts:    coerceStringSlice(raw.AllowedHosts),
		AllowedPorts:    coerceIntSlice(raw.AllowedPorts),
		SecretRefs:      coerceStringSlice(raw.SecretRefs),
		EgressMode:      raw.EgressMode,
		Capabilities:    raw.Capabilities,
	}
	for _, t := range raw.Tools {
		args.Tools = append(args.Tools, toolSpec{Name: t.Name, Description: t.Description})
	}
	if args.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	fullID, err := resolveProposalID(env, args.ID)
	if err != nil {
		return "", err
	}

	p, err := env.ProposalStore.Get(fullID)
	if err != nil {
		return "", fmt.Errorf("proposal not found: %w", err)
	}
	if p.Status != proposal.StatusDraft && p.Status != proposal.StatusInReview {
		return "", fmt.Errorf("can only update draft or in_review proposals (current status: %s)", p.Status)
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
			// Preserve existing egress mode unless the update explicitly changes it.
			existingMode := ""
			if p.NetworkPolicy != nil {
				existingMode = p.NetworkPolicy.EgressMode
			}
			p.NetworkPolicy = &proposal.ProposalNetworkPolicy{
				DefaultDeny:  true,
				AllowedHosts: result.AllowedHosts,
				AllowedPorts: ports,
				EgressMode:   resolveEgressMode(existingMode, args.EgressMode),
			}
		}
	} else if args.EgressMode != nil && p.NetworkPolicy != nil {
		// Allow updating egress_mode in isolation without regenerating the spec.
		p.NetworkPolicy.EgressMode = *args.EgressMode
	}

	if args.SecretRefs != nil {
		p.SecretsRefs = args.SecretRefs
		// Keep capabilities.secrets in sync.
		if p.Capabilities == nil {
			p.Capabilities = &proposal.SkillCapabilities{}
		}
		p.Capabilities.Secrets = args.SecretRefs
	}

	// Apply explicit capabilities override (merged with existing).
	if args.Capabilities != nil {
		if p.Capabilities == nil {
			p.Capabilities = &proposal.SkillCapabilities{}
		}
		if args.Capabilities.Network {
			p.Capabilities.Network = true
		}
		if len(args.Capabilities.Secrets) > 0 {
			p.Capabilities.Secrets = args.Capabilities.Secrets
		}
		if args.Capabilities.FilesystemWrite {
			p.Capabilities.FilesystemWrite = true
		}
		if len(args.Capabilities.HostDevices) > 0 {
			p.Capabilities.HostDevices = args.Capabilities.HostDevices
		}
	}

	p.BumpVersion()

	if err := env.ProposalStore.Update(p); err != nil {
		return "", fmt.Errorf("failed to save: %w", err)
	}

	return fmt.Sprintf("Draft updated.\n  ID: %s\n  Title: %s\n  Skill: %s\n  Risk: %s",
		p.ID, p.Title, p.TargetSkill, p.Risk), nil
}

// coerceStringSlice parses a json.RawMessage as []string, tolerating common
// LLM mistakes like sending a plain string or a Python-style list literal.
func coerceStringSlice(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	// Attempt to treat as a single string value.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		// Check for Python-style list: "['a', 'b']"
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			// Replace single quotes with double quotes and try JSON parse.
			fixed := strings.ReplaceAll(s, "'", "\"")
			if err := json.Unmarshal([]byte(fixed), &arr); err == nil {
				return arr
			}
		}
		return []string{s}
	}
	return nil
}

// coerceIntSlice parses a json.RawMessage as []int, tolerating string input.
func coerceIntSlice(raw json.RawMessage) []int {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var arr []int
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	// Attempt to treat as a single string containing a list.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			fixed := strings.ReplaceAll(s, "'", "\"")
			if err := json.Unmarshal([]byte(fixed), &arr); err == nil {
				return arr
			}
		}
	}
	return nil
}

// handleProposalGetDraft loads and returns a proposal's details.
func handleProposalGetDraft(env *runtimeEnv, ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	fullID, err := resolveProposalID(env, args.ID)
	if err != nil {
		return "", err
	}

	p, err := env.ProposalStore.Get(fullID)
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
func handleProposalListDrafts(env *runtimeEnv, ctx context.Context) (string, error) {
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
			s.ID, truncateStr(s.Title, 28), s.Status, s.Risk, s.Round))
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

	fullID, err := resolveProposalID(env, args.ID)
	if err != nil {
		return "", err
	}

	p, err := env.ProposalStore.Get(fullID)
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

	payload, _ := json.Marshal(map[string]string{
		"proposal_id": p.ID,
		"trace_id":    reActTraceIDFromContext(ctx),
	})
	action := kernel.NewAction(kernel.ActionProposalSubmit, "chat", payload)
	env.Kernel.SignAndLog(action)

	result := fmt.Sprintf("Proposal submitted for court review.\n  ID: %s\n  Title: %s\n  Status: %s\n\nIMPORTANT: Tell the user the proposal ID (%s) so they can track it.", p.ID, p.Title, p.Status, p.ID)

	// Verify the submission was persisted.
	verified, verifyErr := env.ProposalStore.Get(p.ID)
	if verifyErr != nil {
		result += fmt.Sprintf("\n\nWarning: could not verify submission: %v", verifyErr)
	} else if verified.Status == proposal.StatusDraft {
		result += "\n\nWarning: proposal is still in draft status — submission may have failed."
	}

	// Try to start court review via daemon.
	if daemonClient != nil {
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
			result += fmt.Sprintf("\n\nCourt review could not be started automatically: %v\nRecovery: proposal remains in_review and will be auto-resumed on next daemon start (aegisclaw stop; aegisclaw start).\nUse proposal.status / proposal.reviews with ID %s to track progress.", err, p.ID)
		}
	}

	return result, nil
}

// handleProposalStatus checks the current status of a proposal.
func handleProposalStatus(env *runtimeEnv, ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	fullID, err := resolveProposalID(env, args.ID)
	if err != nil {
		return "", err
	}

	p, err := env.ProposalStore.Get(fullID)
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

// handleProposalSubmitDirect transitions a draft to submitted and starts court
// review directly via env.Court (used inside the daemon tool registry where no
// daemon API client is available).
func handleProposalSubmitDirect(env *runtimeEnv, ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	fullID, err := resolveProposalID(env, args.ID)
	if err != nil {
		return "", err
	}

	p, err := env.ProposalStore.Get(fullID)
	if err != nil {
		return "", fmt.Errorf("not found: %w", err)
	}
	if p.Status != proposal.StatusDraft {
		return fmt.Sprintf("Proposal is already %s (not draft).", p.Status), nil
	}

	if err := p.Transition(proposal.StatusSubmitted, "submitted for court review", "agent"); err != nil {
		return "", fmt.Errorf("transition failed: %w", err)
	}
	if err := env.ProposalStore.Update(p); err != nil {
		return "", fmt.Errorf("failed to save: %w", err)
	}

	payload, _ := json.Marshal(map[string]string{
		"proposal_id": p.ID,
		"trace_id":    reActTraceIDFromContext(ctx),
	})
	action := kernel.NewAction(kernel.ActionProposalSubmit, "agent", payload)
	env.Kernel.SignAndLog(action)

	result := fmt.Sprintf("Proposal submitted for court review.\n  ID: %s\n  Title: %s\n  Status: %s\n\nIMPORTANT: Tell the user the proposal ID (%s) so they can track it.", p.ID, p.Title, p.Status, p.ID)

	// Trigger court review inline if the court engine is available.
	if env.Court != nil {
		session, reviewErr := env.Court.Review(ctx, p.ID)
		if reviewErr != nil {
			result += fmt.Sprintf("\n\nCourt review could not start automatically: %v\nRecovery: proposal remains in_review and will be auto-resumed on next daemon start (aegisclaw stop; aegisclaw start).\nUse proposal.status / proposal.reviews with ID %s to track progress.", reviewErr, p.ID)
		} else {
			result += fmt.Sprintf("\n\nCourt review completed.\n  State: %s\n  Verdict: %s\n  Risk: %.1f",
				session.State, session.Verdict, session.RiskScore)
			env.Logger.Info("court review completed (direct)",
				zap.String("proposal_id", p.ID),
				zap.String("verdict", session.Verdict),
				zap.String("state", string(session.State)),
				zap.Float64("risk_score", session.RiskScore),
			)

			// Auto-transition approved proposals to trigger builder (aligns direct path with API handler)
			if session.Verdict == "approved" {
				updatedP, pErr := env.ProposalStore.Get(p.ID)
				if pErr == nil && updatedP.Status == proposal.StatusApproved {
					if tErr := updatedP.Transition(proposal.StatusImplementing, "auto-triggered by court approval (direct)", "daemon"); tErr == nil {
						if uErr := env.ProposalStore.Update(updatedP); uErr == nil {
							env.Logger.Info("proposal auto-transitioned to implementing (direct)",
								zap.String("proposal_id", p.ID),
								zap.String("status", string(updatedP.Status)),
							)
						} else {
							env.Logger.Warn("failed to update proposal after transition (direct)", zap.Error(uErr))
						}
					} else {
						env.Logger.Warn("failed to transition proposal to implementing (direct)", zap.Error(tErr))
					}
				}
			}
		}
	}

	return result, nil
}

// handleProposalReviews returns detailed review feedback for a proposal.
func handleProposalReviews(env *runtimeEnv, ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	fullID, err := resolveProposalID(env, args.ID)
	if err != nil {
		return "", err
	}

	p, err := env.ProposalStore.Get(fullID)
	if err != nil {
		return "", fmt.Errorf("not found: %w", err)
	}

	if len(p.Reviews) == 0 {
		return fmt.Sprintf("Proposal %s has no reviews yet.\n  Status: %s", p.ID[:8], p.Status), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Proposal %s — %s\n", p.ID[:8], p.Title)
	fmt.Fprintf(&b, "  Status: %s  Risk: %s  Rounds: %d\n\n", p.Status, p.Risk, p.Round)

	// Group reviews by round.
	maxRound := 0
	for _, r := range p.Reviews {
		if r.Round > maxRound {
			maxRound = r.Round
		}
	}
	for round := 1; round <= maxRound; round++ {
		reviews := p.ReviewsForRound(round)
		if len(reviews) == 0 {
			continue
		}
		fmt.Fprintf(&b, "Round %d:\n", round)
		for _, r := range reviews {
			fmt.Fprintf(&b, "  %s (%s): %s  risk=%.1f\n", r.Persona, r.Model, r.Verdict, r.RiskScore)
			if r.Comments != "" {
				fmt.Fprintf(&b, "    Comment: %s\n", r.Comments)
			}
			for _, q := range r.Questions {
				fmt.Fprintf(&b, "    Question: %s\n", q)
			}
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// handleProposalVote casts a human vote on a proposal via the court engine.
func handleProposalVote(env *runtimeEnv, ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		ID      string `json:"id"`
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.ID == "" {
		return "", fmt.Errorf("id is required")
	}
	if args.Reason == "" {
		return "", fmt.Errorf("reason is required")
	}

	fullID, err := resolveProposalID(env, args.ID)
	if err != nil {
		return "", err
	}

	if env.Court == nil {
		return "", fmt.Errorf("court engine not available")
	}

	session, err := env.Court.VoteOnProposal(ctx, fullID, "chat-user", args.Approve, args.Reason)
	if err != nil {
		return "", fmt.Errorf("vote failed: %w", err)
	}

	action := "approved"
	if !args.Approve {
		action = "rejected"
	}
	return fmt.Sprintf("Vote recorded: %s\n  Proposal: %s\n  Verdict: %s\n  State: %s",
		action, fullID[:8], session.Verdict, session.State), nil
}
