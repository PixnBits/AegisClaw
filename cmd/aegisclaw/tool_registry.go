package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ToolHandler is a function that executes a named tool and returns a human-readable result.
type ToolHandler func(ctx context.Context, argsJSON string) (string, error)

// ToolRegistry maps qualified tool names to handler functions.
// It is populated at daemon startup and used by the chat message handler to
// dispatch tool.exec requests from the agent VM.
type ToolRegistry struct {
	env      *runtimeEnv
	handlers map[string]ToolHandler
}

// Register adds a handler for the given qualified tool name (e.g. "proposal.create_draft").
func (r *ToolRegistry) Register(name string, h ToolHandler) {
	if r.handlers == nil {
		r.handlers = make(map[string]ToolHandler)
	}
	r.handlers[name] = h
}

// Execute dispatches a tool call by name and returns the result string.
// For unknown names with a <skill>.<tool> pattern, it falls through to skill
// VM dispatch. Returns an error if the tool is not registered and not a skill tool.
func (r *ToolRegistry) Execute(ctx context.Context, tool, argsJSON string) (string, error) {
	h, ok := r.handlers[tool]
	if ok {
		return h(ctx, argsJSON)
	}

	// Fall through to skill VM dispatch for <skillname>.<toolname> patterns.
	skill, skillTool := parseSkillToolName(tool)
	if skill != "" && skillTool != "" {
		return r.invokeSkillTool(ctx, skill, skillTool, argsJSON)
	}

	return "", fmt.Errorf("unknown tool: %s", tool)
}

// invokeSkillTool sends a tool.invoke request to the skill's sandbox VM.
func (r *ToolRegistry) invokeSkillTool(ctx context.Context, skill, tool, argsJSON string) (string, error) {
	if r.env.SafeMode.Load() {
		return "", fmt.Errorf("safe mode is active: skill invocation blocked")
	}
	entry, ok := r.env.Registry.Get(skill)
	if !ok {
		return "", fmt.Errorf("skill %q not found", skill)
	}
	if entry.State != sandbox.SkillStateActive {
		return "", fmt.Errorf("skill %q is not active (state: %s)", skill, entry.State)
	}

	vmReq := map[string]interface{}{
		"id":   uuid.New().String(),
		"type": "tool.invoke",
		"payload": map[string]string{
			"tool": tool,
			"args": argsJSON,
		},
	}
	raw, err := r.env.Runtime.SendToVM(ctx, entry.SandboxID, vmReq)
	if err != nil {
		return "", fmt.Errorf("vsock invoke: %w", err)
	}

	var vmResp struct {
		Success bool            `json:"success"`
		Error   string          `json:"error,omitempty"`
		Data    json.RawMessage `json:"data,omitempty"`
	}
	if err := json.Unmarshal(raw, &vmResp); err != nil {
		return "", fmt.Errorf("parse VM response: %w", err)
	}
	if !vmResp.Success {
		return "", fmt.Errorf("tool failed: %s", vmResp.Error)
	}

	// Data is the ToolInvokeResult JSON from the guest-agent.
	var result struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(vmResp.Data, &result); err != nil {
		return string(vmResp.Data), nil
	}
	return result.Output, nil
}

// Names returns all explicitly registered tool names (does not include skill wildcard).
func (r *ToolRegistry) Names() []string {
	names := make([]string, 0, len(r.handlers))
	for n := range r.handlers {
		names = append(names, n)
	}
	return names
}

// parseSkillToolName splits "skillname.toolname" into skill and tool parts,
// rejecting known non-skill prefixes.
func parseSkillToolName(name string) (skill, tool string) {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) != 2 {
		return "", ""
	}
	switch parts[0] {
	case "list", "proposal":
		return "", ""
	}
	return parts[0], parts[1]
}

// buildToolRegistry constructs the daemon's tool registry with all proposal handlers
// and inline implementations for listing/activating resources.
func buildToolRegistry(env *runtimeEnv) *ToolRegistry {
	reg := &ToolRegistry{env: env}

	reg.Register("proposal.create_draft", func(_ context.Context, args string) (string, error) {
		return handleProposalCreateDraft(env, args)
	})
	reg.Register("proposal.update_draft", func(_ context.Context, args string) (string, error) {
		return handleProposalUpdateDraft(env, args)
	})
	reg.Register("proposal.get_draft", func(_ context.Context, args string) (string, error) {
		return handleProposalGetDraft(env, args)
	})
	reg.Register("proposal.list_drafts", func(_ context.Context, _ string) (string, error) {
		return handleProposalListDrafts(env)
	})
	reg.Register("proposal.submit", func(ctx context.Context, args string) (string, error) {
		return handleProposalSubmitDirect(env, ctx, args)
	})
	reg.Register("proposal.status", func(_ context.Context, args string) (string, error) {
		return handleProposalStatus(env, args)
	})
	reg.Register("proposal.reviews", func(_ context.Context, args string) (string, error) {
		return handleProposalReviews(env, args)
	})
	reg.Register("proposal.vote", func(ctx context.Context, args string) (string, error) {
		return handleProposalVote(env, ctx, args)
	})

	reg.Register("list_proposals", func(_ context.Context, _ string) (string, error) {
		summaries, err := env.ProposalStore.List()
		if err != nil {
			return "", fmt.Errorf("list proposals: %w", err)
		}
		if len(summaries) == 0 {
			return "No proposals found.", nil
		}
		var lines []string
		for _, s := range summaries {
			lines = append(lines, fmt.Sprintf("  %s  %s  [%s]  %s", s.ID, s.Title, s.Status, s.Risk))
		}
		return strings.Join(lines, "\n"), nil
	})

	reg.Register("list_sandboxes", func(ctx context.Context, _ string) (string, error) {
		sandboxes, err := env.Runtime.List(ctx)
		if err != nil {
			return "", fmt.Errorf("list sandboxes: %w", err)
		}
		if len(sandboxes) == 0 {
			return "No sandboxes found.", nil
		}
		var lines []string
		for _, sb := range sandboxes {
			lines = append(lines, fmt.Sprintf("  %s  %s  [%s]", sb.Spec.ID[:8], sb.Spec.Name, sb.State))
		}
		return strings.Join(lines, "\n"), nil
	})

	reg.Register("list_skills", func(_ context.Context, _ string) (string, error) {
		skills := env.Registry.List()
		if len(skills) == 0 {
			return "No skills registered.", nil
		}
		var lines []string
		for _, sk := range skills {
			lines = append(lines, fmt.Sprintf("  %s  [%s]  sandbox=%s  version=%d", sk.Name, sk.State, sk.SandboxID[:8], sk.Version))
		}
		return "Skills:\n" + strings.Join(lines, "\n"), nil
	})

	reg.Register("activate_skill", func(ctx context.Context, args string) (string, error) {
		var params struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(args), &params); err != nil {
			// Allow bare string as the skill name.
			params.Name = strings.TrimSpace(args)
		}
		if params.Name == "" {
			return "", fmt.Errorf("skill name is required (args: {\"name\": \"skill-name\"})")
		}
		// Delegate to the same handler used by the skill.activate API endpoint.
		reqData, _ := json.Marshal(map[string]string{"name": params.Name})
		resp := makeSkillActivateHandler(env)(ctx, reqData)
		if !resp.Success {
			return "", fmt.Errorf("activation failed: %s", resp.Error)
		}
		var result map[string]interface{}
		if json.Unmarshal(resp.Data, &result) == nil {
			return fmt.Sprintf("Skill %q activated.\n  Sandbox: %s\n  Version: %v\n  Hash: %v",
				params.Name, result["sandbox_id"], result["version"], result["hash"]), nil
		}
		return fmt.Sprintf("Skill %q activated.", params.Name), nil
	})

	// ---------------------------------------------------------------------------
	// Phase 2 – conversation.summarize stub (PRD §10.6 A2, architecture.md §8.1).
	//
	// This tool is called automatically at session close to produce a persistent
	// summary of the conversation.  The summary is stored in the JSONL history
	// and used as context for the next session.
	//
	// Full implementation requires a dedicated "conversation.summarize" skill VM
	// with a Court-approved proposal.  The stub logs the request to the Merkle
	// chain so all summarization attempts are auditable.
	// ---------------------------------------------------------------------------

	reg.Register("conversation.summarize", func(ctx context.Context, args string) (string, error) {
		var params struct {
			SessionID string `json:"session_id"` // optional identifier for the session
			MaxTokens int    `json:"max_tokens"`  // optional output length hint
		}
		_ = json.Unmarshal([]byte(args), &params)

		// Audit-log the summarization request even though the skill is not yet implemented.
		if env.Kernel != nil {
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"session_id": params.SessionID,
				"max_tokens": params.MaxTokens,
			})
			env.Kernel.SignAndLog(kernel.NewAction(kernel.ActionAgentConversationSummarize, "tool", auditPayload))
		}
		return "", fmt.Errorf("conversation.summarize is not yet implemented — " +
			"see PRD §10.6 A2 and docs/prd-deviations.md for the roadmap")
	})

	// ---------------------------------------------------------------------------
	// Phase 3 stubs – event-driven and scheduled goals (PRD §10.6 A3).
	//
	// These handlers are scaffolding only.  Full implementation requires:
	//  - A dedicated Orchestrator microVM (architecture.md §8.2 A3).
	//  - Court-reviewed proposals for each skill (see docs/PRD.md §10.6).
	//  - New ACL entries for RoleOrchestrator in internal/ipc/acl.go.
	//
	// Each stub:
	//  - Validates its JSON arguments and reports schema errors clearly.
	//  - Logs the attempt to the Merkle audit chain (fully auditable from day 1).
	//  - Returns a clear "not yet implemented" message for the agent to relay.
	// ---------------------------------------------------------------------------

	// schedule.create registers a cron-style recurring trigger.
	// Expected args: {"cron": "0 9 * * 1-5", "goal": "brief task description", "model": "optional-model"}
	// Future: sends a registration message to the Orchestrator microVM which
	// injects chat.message events into AegisHub at the scheduled time.
	reg.Register("schedule.create", func(ctx context.Context, args string) (string, error) {
		var params struct {
			Cron  string `json:"cron"`
			Goal  string `json:"goal"`
			Model string `json:"model"`
		}
		if err := json.Unmarshal([]byte(args), &params); err != nil {
			return "", fmt.Errorf("schedule.create: invalid args (expected {\"cron\":\"...\",\"goal\":\"...\"}): %w", err)
		}
		if params.Cron == "" {
			return "", fmt.Errorf("schedule.create: \"cron\" field is required (e.g. \"0 9 * * 1-5\" for 09:00 Mon–Fri)")
		}
		if params.Goal == "" {
			return "", fmt.Errorf("schedule.create: \"goal\" field is required — describe what the agent should do")
		}

		// Audit-log the registration attempt.
		if env.Kernel != nil {
			auditPayload, _ := json.Marshal(map[string]string{
				"cron":  params.Cron,
				"goal":  params.Goal,
				"model": params.Model,
			})
			env.Kernel.SignAndLog(kernel.NewAction(kernel.ActionEventScheduleCreate, "tool", auditPayload))
		}
		return "", fmt.Errorf("schedule.create is not yet implemented — " +
			"see PRD §10.6 A3 and docs/prd-deviations.md for the roadmap (args validated: cron=%q goal=%q)", params.Cron, params.Goal)
	})

	// webhook.register opens an inbound HTTPS endpoint in an isolated microVM.
	// Expected args: {"path": "/hook/my-event", "goal": "brief task description", "secret_ref": "optional-secret-name"}
	// Future: a dedicated Network Proxy VM accepts the webhook and injects an
	// event.trigger message through AegisHub to the agent VM.
	reg.Register("webhook.register", func(ctx context.Context, args string) (string, error) {
		var params struct {
			Path      string `json:"path"`
			Goal      string `json:"goal"`
			SecretRef string `json:"secret_ref"` // optional HMAC secret name from the secrets vault
		}
		if err := json.Unmarshal([]byte(args), &params); err != nil {
			return "", fmt.Errorf("webhook.register: invalid args (expected {\"path\":\"...\",\"goal\":\"...\"}): %w", err)
		}
		if params.Path == "" {
			return "", fmt.Errorf("webhook.register: \"path\" field is required (e.g. \"/hooks/deploy\")")
		}
		if params.Goal == "" {
			return "", fmt.Errorf("webhook.register: \"goal\" field is required — describe what the agent should do on receipt")
		}
		// Security: secret_ref (if provided) must reference a name in the secrets
		// vault — it is never passed to the LLM or stored in plain text.
		if params.SecretRef != "" && strings.ContainsAny(params.SecretRef, "/ \\\"") {
			return "", fmt.Errorf("webhook.register: \"secret_ref\" must be a simple vault key name (no path separators or quotes)")
		}

		// Audit-log the registration attempt.
		if env.Kernel != nil {
			auditPayload, _ := json.Marshal(map[string]string{
				"path":       params.Path,
				"goal":       params.Goal,
				"secret_ref": params.SecretRef,
			})
			env.Kernel.SignAndLog(kernel.NewAction(kernel.ActionEventWebhookRegister, "tool", auditPayload))
		}
		return "", fmt.Errorf("webhook.register is not yet implemented — " +
			"see PRD §10.6 A3 and docs/prd-deviations.md for the roadmap (args validated: path=%q goal=%q)", params.Path, params.Goal)
	})

	// monitor.start polls an external resource and fires on state change.
	// Expected args: {"target": "http://...", "condition": "status!=200", "goal": "brief task", "interval_secs": 60}
	// Future: runs in its own isolated skill microVM; fires event.trigger via
	// AegisHub → Orchestrator → agent VM on detected change.
	reg.Register("monitor.start", func(ctx context.Context, args string) (string, error) {
		var params struct {
			Target       string `json:"target"`        // URL or resource to poll
			Condition    string `json:"condition"`     // trigger condition description
			Goal         string `json:"goal"`          // what the agent should do on trigger
			IntervalSecs int    `json:"interval_secs"` // polling interval (default 60)
		}
		if err := json.Unmarshal([]byte(args), &params); err != nil {
			return "", fmt.Errorf("monitor.start: invalid args (expected {\"target\":\"...\",\"condition\":\"...\",\"goal\":\"...\"}): %w", err)
		}
		if params.Target == "" {
			return "", fmt.Errorf("monitor.start: \"target\" field is required (URL or resource to monitor)")
		}
		if params.Condition == "" {
			return "", fmt.Errorf("monitor.start: \"condition\" field is required (e.g. \"response_code!=200\")")
		}
		if params.Goal == "" {
			return "", fmt.Errorf("monitor.start: \"goal\" field is required — describe what the agent should do when condition fires")
		}
		// Security: ensure no secrets appear in the target URL (a monitoring VM
		// will poll this URL from inside its own isolated network namespace).
		if strings.ContainsAny(params.Target, "?#") {
			// Warn but don't block — query params may be legitimate.
			env.Logger.Warn("monitor.start: target URL contains query parameters — ensure no secrets are embedded",
				zap.String("target_prefix", params.Target[:min(len(params.Target), 40)]))
		}

		// Audit-log the registration attempt.
		if env.Kernel != nil {
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"target":        params.Target,
				"condition":     params.Condition,
				"goal":          params.Goal,
				"interval_secs": params.IntervalSecs,
			})
			env.Kernel.SignAndLog(kernel.NewAction(kernel.ActionEventMonitorStart, "tool", auditPayload))
		}
		return "", fmt.Errorf("monitor.start is not yet implemented — " +
			"see PRD §10.6 A3 and docs/prd-deviations.md for the roadmap (args validated: target=%q condition=%q)", params.Target, params.Condition)
	})

	return reg
}
