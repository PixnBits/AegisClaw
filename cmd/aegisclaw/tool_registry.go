package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/google/uuid"
)

// ToolHandler is a function that executes a named tool and returns a human-readable result.
type ToolHandler func(ctx context.Context, argsJSON string) (string, error)

// ToolMeta holds the registration metadata for a single tool.
type ToolMeta struct {
	Name        string
	Description string
	Handler     ToolHandler
}

// ToolRegistry maps qualified tool names to handler functions.
// It is populated at daemon startup and used by the chat message handler to
// dispatch tool.exec requests from the agent VM.
type ToolRegistry struct {
	env      *runtimeEnv
	handlers map[string]ToolHandler
	meta     map[string]ToolMeta
}

// Register adds a handler for the given qualified tool name (e.g. "proposal.create_draft").
// The description is used by search_tools for semantic/keyword matching.
func (r *ToolRegistry) Register(name, description string, h ToolHandler) {
	if r.handlers == nil {
		r.handlers = make(map[string]ToolHandler)
	}
	if r.meta == nil {
		r.meta = make(map[string]ToolMeta)
	}
	r.handlers[name] = h
	r.meta[name] = ToolMeta{Name: name, Description: description, Handler: h}
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

// SearchTools returns tool metadata whose name or description contains any of
// the query keywords (case-insensitive).  If query is empty all tools are returned.
// This implements the semantic + keyword search required by Phase 0.
func (r *ToolRegistry) SearchTools(query string) []ToolMeta {
	query = strings.ToLower(strings.TrimSpace(query))
	var results []ToolMeta

	for _, m := range r.meta {
		if query == "" {
			results = append(results, m)
			continue
		}
		// Keyword matching: split query on whitespace and match any token.
		tokens := strings.Fields(query)
		nameLower := strings.ToLower(m.Name)
		descLower := strings.ToLower(m.Description)
		for _, tok := range tokens {
			if strings.Contains(nameLower, tok) || strings.Contains(descLower, tok) {
				results = append(results, m)
				break
			}
		}
	}
	return results
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

	reg.Register("proposal.create_draft",
		"Create a new skill proposal draft. args: {title, description, skill_name, tools, intended_user, example_usage, risk_assessment, dependencies, tests, security_considerations}",
		func(_ context.Context, args string) (string, error) {
			return handleProposalCreateDraft(env, args)
		})
	reg.Register("proposal.update_draft",
		"Update fields on an existing draft or in-review proposal. args: {id, ...fields}",
		func(_ context.Context, args string) (string, error) {
			return handleProposalUpdateDraft(env, args)
		})
	reg.Register("proposal.get_draft",
		"Retrieve full details of a proposal draft. args: {id}",
		func(_ context.Context, args string) (string, error) {
			return handleProposalGetDraft(env, args)
		})
	reg.Register("proposal.list_drafts",
		"List all proposal drafts.",
		func(_ context.Context, _ string) (string, error) {
			return handleProposalListDrafts(env)
		})
	reg.Register("proposal.submit",
		"Submit a draft proposal for Governance Court review. args: {id}",
		func(ctx context.Context, args string) (string, error) {
			return handleProposalSubmitDirect(env, ctx, args)
		})
	reg.Register("proposal.status",
		"Check the current status and stage of a proposal. args: {id}",
		func(_ context.Context, args string) (string, error) {
			return handleProposalStatus(env, args)
		})
	reg.Register("proposal.reviews",
		"Get detailed reviewer feedback (verdicts, comments, questions) for a proposal. args: {id}",
		func(_ context.Context, args string) (string, error) {
			return handleProposalReviews(env, args)
		})
	reg.Register("proposal.vote",
		"Cast a human vote to approve or reject an escalated proposal. args: {id, approve, reason}",
		func(ctx context.Context, args string) (string, error) {
			return handleProposalVote(env, ctx, args)
		})

	reg.Register("list_proposals",
		"List all proposals with their title, status, and risk level.",
		func(_ context.Context, _ string) (string, error) {
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

	reg.Register("list_sandboxes",
		"List all Firecracker microVM sandboxes and their current state.",
		func(ctx context.Context, _ string) (string, error) {
			sandboxes, err := env.Runtime.List(ctx)
			if err != nil {
				return "", fmt.Errorf("list sandboxes: %w", err)
			}
			if len(sandboxes) == 0 {
				return "No sandboxes found.", nil
			}
			var lines []string
			for _, sb := range sandboxes {
				id := sb.Spec.ID
				if len(id) > 8 {
					id = id[:8]
				}
				lines = append(lines, fmt.Sprintf("  %s  %s  [%s]", id, sb.Spec.Name, sb.State))
			}
			return strings.Join(lines, "\n"), nil
		})

	reg.Register("list_skills",
		"List all registered skills and their activation state.",
		func(_ context.Context, _ string) (string, error) {
			skills := env.Registry.List()
			if len(skills) == 0 {
				return "No skills registered.", nil
			}
			var lines []string
			for _, sk := range skills {
				sandboxID := sk.SandboxID
				if len(sandboxID) > 8 {
					sandboxID = sandboxID[:8]
				}
				lines = append(lines, fmt.Sprintf("  %s  [%s]  sandbox=%s  version=%d", sk.Name, sk.State, sandboxID, sk.Version))
			}
			return "Skills:\n" + strings.Join(lines, "\n"), nil
		})

	reg.Register("activate_skill",
		"Activate an approved skill so its tools become available. args: {name}",
		func(ctx context.Context, args string) (string, error) {
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

	reg.Register("search_tools",
		"Search available tools by keyword. Returns tool names and descriptions matching the query. args: {query}",
		func(_ context.Context, args string) (string, error) {
			var params struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				params.Query = strings.TrimSpace(args)
			}
			matches := reg.SearchTools(params.Query)
			if len(matches) == 0 {
				return fmt.Sprintf("No tools found matching %q.", params.Query), nil
			}
			var lines []string
			for _, m := range matches {
				lines = append(lines, fmt.Sprintf("  %-35s %s", m.Name, m.Description))
			}
			return fmt.Sprintf("Tools matching %q:\n%s", params.Query, strings.Join(lines, "\n")), nil
		})

	reg.Register("snapshot.create",
		"Create a Firecracker snapshot (memory + VM state) of the running agent VM. args: {label}",
		func(ctx context.Context, args string) (string, error) {
			var params struct {
				Label string `json:"label"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				params.Label = strings.TrimSpace(args)
			}
			if params.Label == "" {
				params.Label = "agent-baseline"
			}
			env.agentVMMu.Lock()
			vmID := env.AgentVMID
			env.agentVMMu.Unlock()
			if vmID == "" {
				return "", fmt.Errorf("agent VM is not running; start a chat session first")
			}
			meta, err := env.Runtime.CreateSnapshot(ctx, vmID, params.Label, env.Config.Snapshot.Dir)
			if err != nil {
				return "", fmt.Errorf("create snapshot: %w", err)
			}
			return fmt.Sprintf("Snapshot %q created.\n  VM: %s\n  Files: %s, %s\n  Created: %s",
				meta.Label, meta.VMID, meta.SnapFile, meta.MemFile, meta.CreatedAt.Format(time.RFC3339)), nil
		})

	reg.Register("snapshot.list",
		"List all available agent VM snapshots.",
		func(_ context.Context, _ string) (string, error) {
			metas, err := sandbox.ListSnapshots(env.Config.Snapshot.Dir)
			if err != nil {
				return "", fmt.Errorf("list snapshots: %w", err)
			}
			if len(metas) == 0 {
				return "No snapshots found.", nil
			}
			var lines []string
			for _, m := range metas {
				vmID := m.VMID
				if len(vmID) > 8 {
					vmID = vmID[:8]
				}
				lines = append(lines, fmt.Sprintf("  %-20s  vm=%-12s  created=%s",
					m.Label, vmID, m.CreatedAt.Format("2006-01-02 15:04:05")))
			}
			return "Snapshots:\n" + strings.Join(lines, "\n"), nil
		})

	reg.Register("snapshot.restore",
		"Restore the agent VM from a named snapshot. The current agent VM is stopped and replaced. args: {label}",
		func(ctx context.Context, args string) (string, error) {
			var params struct {
				Label string `json:"label"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				params.Label = strings.TrimSpace(args)
			}
			if params.Label == "" {
				params.Label = "agent-baseline"
			}

			meta, err := sandbox.LoadSnapshotMeta(env.Config.Snapshot.Dir, params.Label)
			if err != nil {
				return "", fmt.Errorf("load snapshot metadata: %w", err)
			}

			// Stop and delete the current agent VM if running.
			env.agentVMMu.Lock()
			oldVMID := env.AgentVMID
			env.AgentVMID = ""
			env.agentVMMu.Unlock()

			if oldVMID != "" {
				env.LLMProxy.StopForVM(oldVMID)
				_ = env.Runtime.Stop(ctx, oldVMID)
				_ = env.Runtime.Delete(ctx, oldVMID)
			}

			newSpec := meta.OriginalSpec
			newSpec.ID = generateVMID("agent")
			newSpec.Name = "aegisclaw-agent"

			newVMID, err := env.Runtime.RestoreSnapshot(ctx, meta, newSpec)
			if err != nil {
				return "", fmt.Errorf("restore snapshot: %w", err)
			}

			// Re-attach the LLM proxy to the new VM.
			vsockPath, err := env.Runtime.VsockPath(newVMID)
			if err != nil {
				return newVMID, fmt.Errorf("get vsock path for restored VM: %w", err)
			}
			if err := env.LLMProxy.StartForVM(newVMID, vsockPath); err != nil {
				return newVMID, fmt.Errorf("start LLM proxy for restored VM: %w", err)
			}

			env.agentVMMu.Lock()
			env.AgentVMID = newVMID
			env.agentVMMu.Unlock()

			return fmt.Sprintf("Agent VM restored from snapshot %q.\n  New VM ID: %s", params.Label, newVMID), nil
		})

	return reg
}
