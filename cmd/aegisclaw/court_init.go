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
		engine.SetRoundUpdater(makeCourtRoundUpdater(env))
	}

	return engine, nil
}

// makeCourtRoundUpdater returns an updater that sends aggregated feedback to
// the agent VM and waits for the agent to update the proposal.  The full ReAct
// loop runs inside the agent VM (D2-a resolved): the agent calls Ollama, parses
// tool-call blocks, executes tools via the tool proxy, and returns a final
// response.  The round updater only needs to verify that the proposal version
// advanced.
func makeCourtRoundUpdater(env *runtimeEnv) court.RoundUpdateFunc {
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

		// Send a single chat.message — the agent VM runs the full ReAct loop
		// internally, calling tools via the tool proxy over vsock.
		payloadBytes, _ := json.Marshal(agentChatPayload{Messages: msgs, Model: env.Config.Ollama.DefaultModel})
		vmReq := agentVMRequest{
			ID:      uuid.New().String(),
			Type:    "chat.message",
			Payload: json.RawMessage(payloadBytes),
		}

		env.Logger.Info("court round updater: sending to agent VM",
			zap.String("proposal_id", p.ID),
			zap.Int("version_before", beforeVersion),
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

		// Check if the proposal was updated during the agent's ReAct loop.
		updated, err := env.ProposalStore.Get(p.ID)
		if err != nil {
			return nil, fmt.Errorf("proposal reload failed after agent response: %w", err)
		}
		if updated.Version > beforeVersion {
			env.Logger.Info("agent updated proposal after court round",
				zap.String("proposal_id", p.ID),
				zap.Int("version_before", beforeVersion),
				zap.Int("version_after", updated.Version),
			)
			return updated, nil
		}

		env.Logger.Error("court round updater: agent did not update proposal",
			zap.String("proposal_id", p.ID),
			zap.Int("version", updated.Version),
		)
		return nil, fmt.Errorf("proposal not updated by agent (version stayed at %d)", updated.Version)
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
