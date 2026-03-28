package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/google/uuid"
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

	return reg
}
