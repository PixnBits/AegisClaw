package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// initCourtEngine initializes the Governance Court engine for the daemon.
// Court operations are managed entirely by the daemon; they are not directly
// accessible via top-level CLI commands per the PRD alignment plan.
//
// D1 (resolved): The only supported launcher is FirecrackerLauncher, which
// runs each reviewer persona in an isolated microVM. The daemon will fail to
// start if KVM or Firecracker is unavailable. DirectLauncher is no longer
// used in production builds.
func initCourtEngine(env *runtimeEnv, toolRegistry *ToolRegistry) (*court.Engine, error) {
	personaDir := env.Config.Court.PersonaDir
	if personaDir == "" {
		var err error
		personaDir, err = court.DefaultPersonaDir()
		if err != nil {
			return nil, fmt.Errorf("failed to determine default persona dir: %w", err)
		}
	}

	personas, err := court.LoadPersonas(personaDir, env.Logger)
	if err != nil {
		// Try to create defaults if dir doesn't exist.
		var createDir string
		createDir, err = court.EnsureDefaultPersonas(env.Logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create default personas: %w", err)
		}
		personaDir = createDir
		personas, err = court.LoadPersonas(personaDir, env.Logger)
		if err != nil {
			return nil, fmt.Errorf("failed to load personas after creating defaults: %w", err)
		}
	}

	launcher, err := initCourtLauncher(env)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize court launcher: %w", err)
	}
	reviewer := court.NewReviewer(launcher, 2, env.Logger)
	reviewerFn := court.NewReviewerFunc(reviewer)

	cfg := court.DefaultEngineConfig()
	engine, err := court.NewEngine(cfg, env.ProposalStore, env.Kernel, personas, reviewerFn, env.Logger, env.Config.Audit.Dir, env.Config.Court.SessionDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create court engine: %w", err)
	}
	if toolRegistry != nil {
		engine.SetRoundUpdater(makeCourtRoundUpdater(env, toolRegistry))
	}

	return engine, nil
}

// makeCourtRoundUpdater returns an updater that notifies the agent after each
// non-consensus round and blocks the next round until the proposal is updated.
// This runs in daemon context, so it works even when no interactive
// `./aegisclaw chat` session is active.
func makeCourtRoundUpdater(env *runtimeEnv, toolRegistry *ToolRegistry) court.RoundUpdateFunc {
	return func(ctx context.Context, p *proposal.Proposal, feedback *court.IterationFeedback) (*proposal.Proposal, error) {
		if p == nil {
			return nil, fmt.Errorf("proposal is nil")
		}

		agentVMID, err := ensureAgentVM(ctx, env)
		if err != nil {
			return nil, fmt.Errorf("agent VM unavailable for court update: %w", err)
		}

		beforeVersion := p.Version
		feedbackText := ""
		if feedback != nil {
			feedbackText = feedback.FormatFeedbackPrompt()
		}
		if feedbackText == "" {
			feedbackText = "No structured questions were extracted; inspect reviewer comments and apply concrete improvements."
		}

		systemPrompt := buildRoundUpdaterSystemPrompt()
		userPrompt := buildRoundUpdaterUserPrompt(p, feedbackText)

		msgs := []agentChatMsg{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		}

		allowedTools := map[string]bool{
			"proposal.get_draft":    true,
			"proposal.reviews":      true,
			"proposal.update_draft": true,
		}

		nudged := false // track whether we already retried after a "final" without update

		for i := 0; i < reactMaxIterationsDefault; i++ {
			payloadBytes, _ := json.Marshal(agentChatPayload{Messages: msgs, Model: env.Config.Ollama.DefaultModel})
			vmReq := agentVMRequest{
				ID:      uuid.New().String(),
				Type:    "chat.message",
				Payload: json.RawMessage(payloadBytes),
			}

			env.Logger.Debug("court round updater: sending to agent",
				zap.String("proposal_id", p.ID),
				zap.Int("iteration", i),
				zap.Int("msg_count", len(msgs)),
			)

			raw, err := env.Runtime.SendToVM(ctx, agentVMID, vmReq)
			if err != nil {
				return nil, fmt.Errorf("agent VM chat.message failed: %w", err)
			}

			var vmResp agentVMResponse
			if err := json.Unmarshal(raw, &vmResp); err != nil {
				return nil, fmt.Errorf("malformed agent response: %w", err)
			}
			if !vmResp.Success {
				return nil, fmt.Errorf("agent error: %s", vmResp.Error)
			}

			var chatResp agentChatResponse
			if err := json.Unmarshal(vmResp.Data, &chatResp); err != nil {
				return nil, fmt.Errorf("malformed agent chat response: %w", err)
			}

			env.Logger.Info("court round updater: agent response",
				zap.String("proposal_id", p.ID),
				zap.Int("iteration", i),
				zap.String("status", chatResp.Status),
				zap.String("tool", chatResp.Tool),
			)

			switch chatResp.Status {
			case "final":
				// Check if the proposal was actually updated (could have happened
				// via a prior tool_call iteration in this same loop).
				updated, err := env.ProposalStore.Get(p.ID)
				if err != nil {
					return nil, fmt.Errorf("proposal reload failed after agent final response: %w", err)
				}
				if updated.Version > beforeVersion {
					env.Logger.Info("agent updated proposal after court round",
						zap.String("proposal_id", p.ID),
						zap.Int("version_before", beforeVersion),
						zap.Int("version_after", updated.Version),
					)
					return updated, nil
				}

				// Daemon-side fallback: the guest-agent may have returned "final"
				// even though the LLM content contains a tool-call block (e.g.
				// small models omit the closing ``` fence). Try to extract and
				// execute the tool call ourselves before nudging.
				if tool, args, ok := extractToolCallFromContent(chatResp.Content); ok && allowedTools[tool] {
					env.Logger.Info("court round updater: daemon-side extracted tool call from final content",
						zap.String("proposal_id", p.ID),
						zap.String("tool", tool),
					)
					result, toolErr := toolRegistry.Execute(ctx, tool, args)
					toolResult := ""
					if toolErr != nil {
						toolResult = fmt.Sprintf("Error executing %s: %v", tool, toolErr)
						env.Logger.Warn("court round updater: daemon-side tool execution failed",
							zap.String("tool", tool),
							zap.Error(toolErr),
						)
					} else {
						toolResult = result
					}
					toolCallContent := fmt.Sprintf("```tool-call\n{\"name\": %q, \"args\": %s}\n```", tool, args)
					msgs = append(msgs,
						agentChatMsg{Role: "assistant", Content: toolCallContent},
						agentChatMsg{Role: "tool", Name: tool, Content: toolResult},
					)
					continue
				}

				// Agent responded with text but didn't call the tool.
				// Nudge it once with an explicit instruction.
				if !nudged {
					nudged = true
					env.Logger.Warn("court round updater: agent gave final response without updating proposal, nudging",
						zap.String("proposal_id", p.ID),
						zap.String("agent_content_preview", truncate(chatResp.Content, 200)),
					)
					msgs = append(msgs,
						agentChatMsg{Role: "assistant", Content: chatResp.Content},
						agentChatMsg{Role: "user", Content: buildRoundUpdaterNudge(p.ID)},
					)
					continue
				}

				// Already nudged once and agent still didn't update. Give up.
				env.Logger.Error("court round updater: agent failed to update proposal after nudge",
					zap.String("proposal_id", p.ID),
					zap.String("agent_content_preview", truncate(chatResp.Content, 200)),
				)
				return nil, fmt.Errorf("proposal not updated by agent (version stayed at %d)", updated.Version)

			case "tool_call":
				toolResult := ""
				if !allowedTools[chatResp.Tool] {
					toolResult = fmt.Sprintf("Error: tool %q is not allowed during court round updates. Allowed tools: proposal.get_draft, proposal.reviews, proposal.update_draft.", chatResp.Tool)
				} else {
					env.Logger.Info("court round updater: executing tool",
						zap.String("proposal_id", p.ID),
						zap.String("tool", chatResp.Tool),
					)
					result, toolErr := toolRegistry.Execute(ctx, chatResp.Tool, chatResp.Args)
					if toolErr != nil {
						toolResult = fmt.Sprintf("Error executing %s: %v", chatResp.Tool, toolErr)
						env.Logger.Warn("court round updater: tool execution failed",
							zap.String("tool", chatResp.Tool),
							zap.Error(toolErr),
						)
					} else {
						toolResult = result
					}
				}

				toolCallContent := fmt.Sprintf("```tool-call\n{\"name\": %q, \"args\": %s}\n```", chatResp.Tool, chatResp.Args)
				msgs = append(msgs,
					agentChatMsg{Role: "assistant", Content: toolCallContent},
					agentChatMsg{Role: "tool", Name: chatResp.Tool, Content: toolResult},
				)

			default:
				return nil, fmt.Errorf("unexpected agent status: %q", chatResp.Status)
			}
		}

		return nil, fmt.Errorf("agent did not finish proposal update within %d iterations", reactMaxIterationsDefault)
	}
}

// buildRoundUpdaterSystemPrompt returns a system prompt laser-focused on tool
// usage for the court round updater context. Unlike the interactive chat system
// prompt, this prompt does NOT encourage conversation — it requires the agent
// to use tools to update the proposal.
func buildRoundUpdaterSystemPrompt() string {
	var b strings.Builder
	b.WriteString("You are AegisClaw's automated proposal updater.\n")
	b.WriteString("Your ONLY job is to update a proposal using the tools provided.\n")
	b.WriteString("Do NOT have a conversation. Do NOT explain your reasoning in prose.\n")
	b.WriteString("You MUST call tools to accomplish your task.\n\n")

	b.WriteString("Available tools (use ONLY these):\n")
	b.WriteString("- \"proposal.get_draft\" — get proposal details. args: {\"id\": \"uuid\"}\n")
	b.WriteString("- \"proposal.reviews\" — get reviewer feedback. args: {\"id\": \"uuid\"}\n")
	b.WriteString("- \"proposal.update_draft\" — update the proposal. args: {\"id\": \"uuid\", \"description\": \"new description\", \"title\": \"new title\"}\n")
	b.WriteString("  Only update \"description\" and optionally \"title\". Do NOT send other fields like allowed_hosts or allowed_ports.\n\n")

	b.WriteString("To call a tool, output EXACTLY this format:\n\n")
	b.WriteString("```tool-call\n{\"name\": \"proposal.update_draft\", \"args\": {\"id\": \"<proposal-id>\", \"description\": \"<improved description>\"}}\n```\n\n")
	b.WriteString("CRITICAL RULES:\n")
	b.WriteString("1. You MUST call proposal.update_draft before finishing.\n")
	b.WriteString("2. After the closing ``` fence, STOP. Write nothing else.\n")
	b.WriteString("3. Do NOT call proposal.submit.\n")
	b.WriteString("4. Address reviewer concerns with concrete changes to the proposal fields.\n")
	b.WriteString("5. If you need details first, call proposal.get_draft or proposal.reviews, then call proposal.update_draft.\n")

	return b.String()
}

// buildRoundUpdaterUserPrompt formats the user prompt for a court round update.
func buildRoundUpdaterUserPrompt(p *proposal.Proposal, feedbackText string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Proposal ID: %s\n", p.ID))
	b.WriteString(fmt.Sprintf("Title: %s\n", p.Title))
	b.WriteString(fmt.Sprintf("Current version: %d\n", p.Version))
	b.WriteString(fmt.Sprintf("Description: %s\n\n", p.Description))
	b.WriteString("Reviewer feedback from the completed round:\n")
	b.WriteString(feedbackText)
	b.WriteString("\n\nCall proposal.update_draft now with improvements that address this feedback.")
	return b.String()
}

// buildRoundUpdaterNudge returns an explicit nudge message when the agent
// failed to use the tool on its first attempt.
func buildRoundUpdaterNudge(proposalID string) string {
	var b strings.Builder
	b.WriteString("You did not call a tool. You MUST call proposal.update_draft to proceed.\n")
	b.WriteString("Output EXACTLY this format (fill in the fields):\n\n")
	b.WriteString("```tool-call\n")
	b.WriteString(fmt.Sprintf("{\"name\": \"proposal.update_draft\", \"args\": {\"id\": \"%s\", \"description\": \"<improved description addressing feedback>\"}}\n", proposalID))
	b.WriteString("```\n\n")
	b.WriteString("Do it now. Do NOT write anything else.")
	return b.String()
}

// truncate returns at most maxLen characters of s, appending "…" if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// extractToolCallFromContent is a daemon-side fallback parser that extracts a
// tool call from agent content that was classified as "final" by the guest-agent.
// This handles the case where the LLM produced a fenced tool-call block but the
// guest-agent's parser missed it (e.g. missing closing fence).
func extractToolCallFromContent(content string) (toolName, argsJSON string, found bool) {
	markers := []string{"```tool-call", "```json", "```"}
	for _, marker := range markers {
		start := strings.Index(content, marker)
		if start < 0 {
			continue
		}
		after := content[start+len(marker):]

		// Try with closing fence first, then without.
		end := strings.Index(after, "```")
		var block string
		if end >= 0 {
			block = strings.TrimSpace(after[:end])
		} else {
			block = strings.TrimSpace(after)
		}

		var modern struct {
			Name string          `json:"name"`
			Args json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal([]byte(block), &modern); err == nil && modern.Name != "" {
			argsStr := "{}"
			if len(modern.Args) > 0 {
				argsStr = string(modern.Args)
			}
			return modern.Name, argsStr, true
		}
	}

	// Bare JSON fallback: look for {"name": anywhere in the content.
	if idx := strings.Index(content, `{"name"`); idx >= 0 {
		candidate := content[idx:]
		var modern struct {
			Name string          `json:"name"`
			Args json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal([]byte(candidate), &modern); err == nil && modern.Name != "" {
			argsStr := "{}"
			if len(modern.Args) > 0 {
				argsStr = string(modern.Args)
			}
			return modern.Name, argsStr, true
		}
	}

	return "", "", false
}

// initCourtLauncher returns the FirecrackerLauncher for Court reviewer sandboxes.
//
// D2-c (resolved): DirectLauncher is no longer reachable from this function.
// If KVM or the Firecracker binary is unavailable, initCourtLauncher returns
// an error so the daemon fails fast with a clear message rather than silently
// degrading to unaudited in-process execution.
func initCourtLauncher(env *runtimeEnv) (court.SandboxLauncher, error) {
	kvmAvailable := isKVMAvailable()
	fcAvailable := isFirecrackerAvailable(env.Config.Firecracker.Bin)

	if !kvmAvailable {
		return nil, fmt.Errorf("KVM is not available (/dev/kvm inaccessible); Firecracker-based court review requires KVM")
	}
	if !fcAvailable {
		return nil, fmt.Errorf("Firecracker binary not found at %q; install Firecracker to run court reviews", env.Config.Firecracker.Bin)
	}

	env.Logger.Info("court reviewers will use Firecracker sandboxes (D1 compliant)",
		zap.String("firecracker", env.Config.Firecracker.Bin),
	)
	rtCfg := sandbox.RuntimeConfig{
		FirecrackerBin: env.Config.Firecracker.Bin,
		JailerBin:      env.Config.Jailer.Bin,
		KernelImage:    env.Config.Sandbox.KernelImage,
		RootfsTemplate: env.Config.Rootfs.Template,
		ChrootBaseDir:  env.Config.Sandbox.ChrootBase,
		StateDir:       env.Config.Sandbox.StateDir,
	}
	// Build the per-VM LLM proxy.  The proxy owns the only path from reviewer
	// VMs to Ollama; VMs have no network interface and call the proxy via vsock.
	allowedModels := llm.AllowedModelsFromRegistry()
	proxy := llm.NewOllamaProxy(allowedModels, "", env.Kernel, env.Logger)

	return court.NewFirecrackerLauncher(env.Runtime, rtCfg, proxy, env.Logger), nil
}

// isKVMAvailable checks whether /dev/kvm is accessible.
func isKVMAvailable() bool {
	_, err := os.Stat("/dev/kvm")
	return err == nil
}

// isFirecrackerAvailable checks whether the Firecracker binary exists.
func isFirecrackerAvailable(binPath string) bool {
	if binPath == "" {
		return false
	}
	_, err := os.Stat(binPath)
	return err == nil
}
