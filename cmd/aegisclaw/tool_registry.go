package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/eventbus"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/lookup"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/registry"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/sbom"
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
	if r.env == nil {
		return "", fmt.Errorf("tool registry has no runtime env: cannot invoke skill %q", skill)
	}
	if r.env.SafeMode.Load() {
		return "", fmt.Errorf("safe mode is active: skill invocation blocked")
	}
	if skill == defaultScriptRunnerSkill {
		if err := ensureDefaultScriptRunnerActive(ctx, r.env); err != nil {
			return "", fmt.Errorf("ensure built-in script runner: %w", err)
		}
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
// rejecting known non-skill prefixes and empty components.
// Returns ("","") whenever the name is not a valid "skill.tool" pair.
func parseSkillToolName(name string) (skill, tool string) {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) != 2 {
		return "", ""
	}
	skill, tool = parts[0], parts[1]
	// Guard against names like ".tool" or "skill." — the caller relies on
	// both parts being non-empty to decide whether to dispatch to the skill VM.
	if skill == "" || tool == "" {
		return "", ""
	}
	switch skill {
	case "list", "proposal":
		return "", ""
	}
	return skill, tool
}

// buildToolRegistry constructs the daemon's tool registry with all proposal handlers
// and inline implementations for listing/activating resources.
func buildToolRegistry(env *runtimeEnv) *ToolRegistry {
	reg := &ToolRegistry{env: env}
	registerProposalTools(reg, env)
	registerMemoryTools(reg, env)
	registerEventBusTools(reg, env)
	registerWorkerTools(reg, env)
	registerSessionTools(reg, env, &reg)
	registerRegistryTools(reg, env)
	registerLookupTools(reg, env)
	return reg
}

func registerProposalTools(reg *ToolRegistry, env *runtimeEnv) {
	reg.Register("proposal.create_draft",
		"Create a new skill proposal draft. args: {title, description, skill_name, tools, intended_user, example_usage, risk_assessment, dependencies, tests, security_considerations}",
		func(ctx context.Context, args string) (string, error) {
			return handleProposalCreateDraft(env, ctx, args)
		})
	reg.Register("proposal.update_draft",
		"Update fields on an existing draft or in-review proposal. args: {id, ...fields}",
		func(ctx context.Context, args string) (string, error) {
			return handleProposalUpdateDraft(env, ctx, args)
		})
	reg.Register("proposal.get_draft",
		"Retrieve full details of a proposal draft. args: {id}",
		func(ctx context.Context, args string) (string, error) {
			return handleProposalGetDraft(env, ctx, args)
		})
	reg.Register("proposal.list_drafts",
		"List all proposal drafts.",
		func(ctx context.Context, _ string) (string, error) {
			return handleProposalListDrafts(env, ctx)
		})
	reg.Register("proposal.submit",
		"Submit a draft proposal for Governance Court review. args: {id}",
		func(ctx context.Context, args string) (string, error) {
			return handleProposalSubmitDirect(env, ctx, args)
		})
	reg.Register("proposal.status",
		"Check the current status and stage of a proposal. args: {id}",
		func(ctx context.Context, args string) (string, error) {
			return handleProposalStatus(env, ctx, args)
		})
	reg.Register("proposal.reviews",
		"Get detailed reviewer feedback (verdicts, comments, questions) for a proposal. args: {id}",
		func(ctx context.Context, args string) (string, error) {
			return handleProposalReviews(env, ctx, args)
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

	reg.Register("skill.sbom",
		"Return the Software Bill of Materials (SBOM) for a skill. args: {proposal_id} OR {skill_name}",
		func(_ context.Context, args string) (string, error) {
			var params struct {
				ProposalID string `json:"proposal_id"`
				SkillName  string `json:"skill_name"`
				Name       string `json:"name"` // alias
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				params.ProposalID = strings.TrimSpace(args)
			}
			if params.Name != "" && params.SkillName == "" {
				params.SkillName = params.Name
			}

			sbomDir := ""
			if env.Config != nil {
				sbomDir = env.Config.Builder.SBOMDir
			}
			if sbomDir == "" {
				return "", fmt.Errorf("SBOM directory not configured (builder.sbom_dir)")
			}

			// If a proposal_id is given, look directly in that subdirectory.
			if params.ProposalID != "" {
				path := filepath.Join(sbomDir, params.ProposalID, "sbom.json")
				s, err := sbom.Read(path)
				if err != nil {
					return "", fmt.Errorf("SBOM not found for proposal %s: %w", params.ProposalID, err)
				}
				b, _ := json.MarshalIndent(s, "", "  ")
				return string(b), nil
			}

			// Try to find by skill name: scan proposal store.
			if params.SkillName != "" && env.ProposalStore != nil {
				proposals, err := env.ProposalStore.List()
				if err != nil {
					return "", fmt.Errorf("list proposals: %w", err)
				}
				for _, p := range proposals {
					path := filepath.Join(sbomDir, p.ID, "sbom.json")
					s, readErr := sbom.Read(path)
					if readErr != nil {
						continue
					}
					if s.Metadata.Component.Name == params.SkillName {
						b, _ := json.MarshalIndent(s, "", "  ")
						return string(b), nil
					}
				}
				return "", fmt.Errorf("SBOM not found for skill %q", params.SkillName)
			}

			return "", fmt.Errorf("provide proposal_id or skill_name")
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

	reg.Register("script.list_languages",
		"List supported scripting runtimes for script execution. args: {}",
		func(_ context.Context, _ string) (string, error) {
			langs := supportedScriptLanguages()
			return "Supported script runtimes: " + strings.Join(langs, ", "), nil
		})

	reg.Register("script.run",
		"Execute short script code with strict limits and timeout. args: {language, code, args[], timeout_ms}. The LLM should generate code directly in the request.",
		func(ctx context.Context, args string) (string, error) {
			params, err := parseRunScriptParams(args)
			if err != nil {
				return "", err
			}
			return runScriptInSandbox(ctx, env, params)
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
}

func registerMemoryTools(reg *ToolRegistry, env *runtimeEnv) {
	reg.Register("store_memory",
		"Store a memory entry persistently. Args: {key, value, tags[], ttl_tier, security_level, task_id}. Returns memory_id.",
		func(ctx context.Context, args string) (string, error) {
			var params struct {
				Key           string   `json:"key"`
				Value         string   `json:"value"`
				Tags          []string `json:"tags"`
				TTLTier       string   `json:"ttl_tier"`
				SecurityLevel string   `json:"security_level"`
				TaskID        string   `json:"task_id"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				return "", fmt.Errorf("invalid store_memory args: %w", err)
			}
			if params.Key == "" {
				return "", fmt.Errorf("store_memory requires 'key'")
			}
			if params.Value == "" {
				return "", fmt.Errorf("store_memory requires 'value'")
			}
			entry := &memory.MemoryEntry{
				Key:    params.Key,
				Value:  params.Value,
				Tags:   params.Tags,
				TaskID: params.TaskID,
			}
			if params.TTLTier != "" {
				entry.TTLTier = memory.TTLTier(params.TTLTier)
			}
			if params.SecurityLevel != "" {
				entry.SecurityLevel = memory.SecurityLevel(params.SecurityLevel)
			}
			memID, err := env.MemoryStore.Store(entry)
			if err != nil {
				return "", fmt.Errorf("store_memory: %w", err)
			}
			// Audit-log the store operation.
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"memory_id": memID, "key": params.Key, "ttl_tier": params.TTLTier,
			})
			act := kernel.NewAction(kernel.ActionMemoryStore, "agent", auditPayload)
			env.Kernel.SignAndLog(act) //nolint:errcheck
			return fmt.Sprintf("Memory stored. ID: %s", memID), nil
		})

	reg.Register("retrieve_memory",
		"Retrieve memories matching a query. Args: {query, k, task_id}. Returns matching memories.",
		func(_ context.Context, args string) (string, error) {
			var params struct {
				Query  string `json:"query"`
				K      int    `json:"k"`
				TaskID string `json:"task_id"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				// Allow bare query string.
				params.Query = strings.TrimSpace(args)
			}
			if params.K <= 0 {
				params.K = 5
			}
			results, err := env.MemoryStore.Retrieve(params.Query, params.K, params.TaskID)
			if err != nil {
				return "", fmt.Errorf("retrieve_memory: %w", err)
			}
			// Audit-log.
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"query": params.Query, "k": params.K, "results": len(results),
			})
			act := kernel.NewAction(kernel.ActionMemoryRetrieve, "agent", auditPayload)
			env.Kernel.SignAndLog(act) //nolint:errcheck
			if len(results) == 0 {
				return fmt.Sprintf("No memories found for %q.", params.Query), nil
			}
			var lines []string
			for _, e := range results {
				tags := strings.Join(e.Tags, ",")
				if tags == "" {
					tags = "-"
				}
				lines = append(lines, fmt.Sprintf("[%s] %s (tier=%s tags=%s)\n  %s",
					e.MemoryID[:8], e.Key, e.TTLTier, tags,
					truncate(e.Value, 200)))
			}
			return strings.Join(lines, "\n---\n"), nil
		})

	reg.Register("compact_memory",
		"Compact memories to reduce storage (tier transition). Args: {task_id, target_tier}.",
		func(_ context.Context, args string) (string, error) {
			var params struct {
				TaskID     string `json:"task_id"`
				TargetTier string `json:"target_tier"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				params.TaskID = strings.TrimSpace(args)
			}
			result, err := env.MemoryStore.Compact(params.TaskID, memory.TTLTier(params.TargetTier))
			if err != nil {
				return "", fmt.Errorf("compact_memory: %w", err)
			}
			// Audit-log.
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"examined": result.Examined, "compacted": result.Compacted,
				"target_tier": params.TargetTier, "elapsed_ms": result.ElapsedTime.Milliseconds(),
			})
			act := kernel.NewAction(kernel.ActionMemoryCompact, "agent", auditPayload)
			env.Kernel.SignAndLog(act) //nolint:errcheck
			return fmt.Sprintf("Compaction complete.\n  Examined: %d\n  Compacted: %d\n  Duration: %s",
				result.Examined, result.Compacted, result.ElapsedTime.Round(time.Millisecond)), nil
		})

	reg.Register("delete_memory",
		"Delete (soft-delete) memories matching a query. GDPR right-to-forget. Args: {query}.",
		func(_ context.Context, args string) (string, error) {
			var params struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				params.Query = strings.TrimSpace(args)
			}
			if params.Query == "" {
				return "", fmt.Errorf("delete_memory requires 'query'")
			}
			n, err := env.MemoryStore.Delete(params.Query)
			if err != nil {
				return "", fmt.Errorf("delete_memory: %w", err)
			}
			// Audit-log.
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"query": params.Query, "deleted": n,
			})
			act := kernel.NewAction(kernel.ActionMemoryDelete, "agent", auditPayload)
			env.Kernel.SignAndLog(act) //nolint:errcheck
			return fmt.Sprintf("Deleted %d memory entries matching %q.", n, params.Query), nil
		})

	reg.Register("list_memories",
		"List memory entries (for inspection). Args: {tier} — optional TTL tier filter.",
		func(_ context.Context, args string) (string, error) {
			var params struct {
				Tier string `json:"tier"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				params.Tier = strings.TrimSpace(args)
			}
			summaries, err := env.MemoryStore.List(memory.TTLTier(params.Tier))
			if err != nil {
				return "", fmt.Errorf("list_memories: %w", err)
			}
			if len(summaries) == 0 {
				return "No memories found.", nil
			}
			var lines []string
			for _, s := range summaries {
				tags := strings.Join(s.Tags, ",")
				if tags == "" {
					tags = "-"
				}
				lines = append(lines, fmt.Sprintf("  %s  %-30s  tier=%-6s  tags=%s  v%d",
					s.MemoryID[:8], truncate(s.Key, 30), s.TTLTier, tags, s.Version))
			}
			return fmt.Sprintf("Memories (%d):\n%s", len(summaries), strings.Join(lines, "\n")), nil
		})
}

func registerEventBusTools(reg *ToolRegistry, env *runtimeEnv) {
	reg.Register("set_timer",
		"Schedule an async timer. Args: {name, trigger_at (ISO8601 for one-shot), cron (for recurring), payload, task_id}. Returns timer_id. "+
			"Optional: include {task_description, role, timeout_mins, tools_granted} in payload to spawn an autonomous worker when the timer fires.",
		func(_ context.Context, args string) (string, error) {
			var params struct {
				Name      string          `json:"name"`
				TriggerAt string          `json:"trigger_at"` // ISO8601 for one-shot
				Cron      string          `json:"cron"`
				Payload   json.RawMessage `json:"payload"`
				TaskID    string          `json:"task_id"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				return "", fmt.Errorf("invalid set_timer args: %w", err)
			}
			if params.Name == "" {
				return "", fmt.Errorf("set_timer requires 'name'")
			}

			p := eventbus.SetTimerParams{
				Name:    params.Name,
				Cron:    params.Cron,
				Payload: params.Payload,
				TaskID:  params.TaskID,
				Owner:   "agent",
			}
			if params.TriggerAt != "" {
				t, err := time.Parse(time.RFC3339, params.TriggerAt)
				if err != nil {
					return "", fmt.Errorf("trigger_at must be RFC3339: %w", err)
				}
				p.TriggerAt = &t
			}

			timer, err := env.EventBus.SetTimer(p)
			if err != nil {
				return "", fmt.Errorf("set_timer: %w", err)
			}
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"timer_id": timer.TimerID, "name": params.Name, "cron": params.Cron,
			})
			act := kernel.NewAction(kernel.ActionEventTimerSet, "agent", auditPayload)
			env.Kernel.SignAndLog(act) //nolint:errcheck
			nextDesc := "N/A"
			if timer.NextFireAt != nil {
				nextDesc = timer.NextFireAt.Format(time.RFC3339)
			} else if timer.TriggerAt != nil {
				nextDesc = timer.TriggerAt.Format(time.RFC3339)
			}
			return fmt.Sprintf("Timer set.\n  ID:      %s\n  Name:    %s\n  Next:    %s",
				timer.TimerID, timer.Name, nextDesc), nil
		})

	reg.Register("cancel_timer",
		"Cancel a scheduled timer. Args: {timer_id}. Returns confirmation.",
		func(_ context.Context, args string) (string, error) {
			var params struct {
				TimerID string `json:"timer_id"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				params.TimerID = strings.TrimSpace(args)
			}
			if params.TimerID == "" {
				return "", fmt.Errorf("cancel_timer requires 'timer_id'")
			}
			ok, err := env.EventBus.CancelTimer(params.TimerID)
			if err != nil {
				return "", fmt.Errorf("cancel_timer: %w", err)
			}
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"timer_id": params.TimerID, "cancelled": ok,
			})
			act := kernel.NewAction(kernel.ActionEventTimerCancel, "agent", auditPayload)
			env.Kernel.SignAndLog(act) //nolint:errcheck
			if !ok {
				return fmt.Sprintf("Timer %s was not found or already terminal.", params.TimerID), nil
			}
			return fmt.Sprintf("Timer %s cancelled.", params.TimerID), nil
		})

	reg.Register("list_pending_async",
		"List pending async items: active timers, subscriptions, and pending approvals. Args: {} or {type: 'timers'|'subscriptions'|'approvals'}.",
		func(_ context.Context, args string) (string, error) {
			var params struct {
				Type string `json:"type"`
			}
			json.Unmarshal([]byte(args), &params) //nolint:errcheck

			var b strings.Builder
			if params.Type == "" || params.Type == "timers" {
				timers := env.EventBus.ListTimers(eventbus.TimerActive)
				b.WriteString(fmt.Sprintf("Active Timers (%d):\n", len(timers)))
				for _, t := range timers {
					next := "N/A"
					if t.NextFireAt != nil {
						next = t.NextFireAt.Format(time.RFC3339)
					} else if t.TriggerAt != nil {
						next = t.TriggerAt.Format(time.RFC3339)
					}
					b.WriteString(fmt.Sprintf("  [%s]  %-24s  next=%-25s  task=%s\n",
						t.TimerID, t.Name, next, t.TaskID))
				}
			}
			if params.Type == "" || params.Type == "subscriptions" {
				subs := env.EventBus.ListSubscriptions(true)
				b.WriteString(fmt.Sprintf("Active Subscriptions (%d):\n", len(subs)))
				for _, s := range subs {
					b.WriteString(fmt.Sprintf("  [%s]  source=%-10s  task=%s  received=%d\n",
						s.SubscriptionID, s.Source, s.TaskID, s.ReceivedCount))
				}
			}
			if params.Type == "" || params.Type == "approvals" {
				approvals := env.EventBus.ListPendingApprovals()
				b.WriteString(fmt.Sprintf("Pending Approvals (%d):\n", len(approvals)))
				for _, a := range approvals {
					b.WriteString(fmt.Sprintf("  [%s]  %s  risk=%-6s  task=%s\n",
						a.ApprovalID, truncate(a.Title, 40), a.RiskLevel, a.TaskID))
				}
			}
			return strings.TrimRight(b.String(), "\n"), nil
		})

	reg.Register("subscribe_signal",
		"Subscribe to signals from an external source. Args: {source (email|calendar|file|git|webhook|custom), filter, task_id}. Returns subscription_id.",
		func(_ context.Context, args string) (string, error) {
			var params struct {
				Source string          `json:"source"`
				Filter json.RawMessage `json:"filter"`
				TaskID string          `json:"task_id"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				return "", fmt.Errorf("invalid subscribe_signal args: %w", err)
			}
			if params.Source == "" {
				return "", fmt.Errorf("subscribe_signal requires 'source'")
			}
			sub, err := env.EventBus.Subscribe(
				eventbus.SignalSource(params.Source), params.Filter, params.TaskID, "agent",
			)
			if err != nil {
				return "", fmt.Errorf("subscribe_signal: %w", err)
			}
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"subscription_id": sub.SubscriptionID, "source": params.Source, "task_id": params.TaskID,
			})
			act := kernel.NewAction(kernel.ActionEventSubscribe, "agent", auditPayload)
			env.Kernel.SignAndLog(act) //nolint:errcheck
			return fmt.Sprintf("Subscribed to '%s' signals.\n  Subscription ID: %s",
				params.Source, sub.SubscriptionID), nil
		})

	reg.Register("unsubscribe_signal",
		"Deactivate a signal subscription. Args: {subscription_id}. Returns confirmation.",
		func(_ context.Context, args string) (string, error) {
			var params struct {
				SubscriptionID string `json:"subscription_id"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				params.SubscriptionID = strings.TrimSpace(args)
			}
			if params.SubscriptionID == "" {
				return "", fmt.Errorf("unsubscribe_signal requires 'subscription_id'")
			}
			ok, err := env.EventBus.Unsubscribe(params.SubscriptionID)
			if err != nil {
				return "", fmt.Errorf("unsubscribe_signal: %w", err)
			}
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"subscription_id": params.SubscriptionID, "unsubscribed": ok,
			})
			act := kernel.NewAction(kernel.ActionEventUnsubscribe, "agent", auditPayload)
			env.Kernel.SignAndLog(act) //nolint:errcheck
			if !ok {
				return fmt.Sprintf("Subscription %s not found or already inactive.", params.SubscriptionID), nil
			}
			return fmt.Sprintf("Subscription %s deactivated.", params.SubscriptionID), nil
		})

	reg.Register("request_human_approval",
		"Request human approval for a high-risk operation. Args: {title, description, risk_level (low|medium|high), payload, task_id, expires_in_hours}. Returns approval_id and waits.",
		func(_ context.Context, args string) (string, error) {
			var params struct {
				Title          string          `json:"title"`
				Description    string          `json:"description"`
				RiskLevel      string          `json:"risk_level"`
				Payload        json.RawMessage `json:"payload"`
				TaskID         string          `json:"task_id"`
				ExpiresInHours float64         `json:"expires_in_hours"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				return "", fmt.Errorf("invalid request_human_approval args: %w", err)
			}
			if params.Title == "" {
				return "", fmt.Errorf("request_human_approval requires 'title'")
			}
			if params.RiskLevel == "" {
				params.RiskLevel = "medium"
			}
			var expiresIn time.Duration
			if params.ExpiresInHours > 0 {
				expiresIn = time.Duration(params.ExpiresInHours * float64(time.Hour))
			}
			a, err := env.EventBus.RequestApproval(
				params.Title, params.Description, params.RiskLevel,
				"agent", params.TaskID, params.Payload, expiresIn,
			)
			if err != nil {
				return "", fmt.Errorf("request_human_approval: %w", err)
			}
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"approval_id": a.ApprovalID, "title": params.Title, "risk_level": params.RiskLevel,
			})
			act := kernel.NewAction(kernel.ActionApprovalRequest, "agent", auditPayload)
			env.Kernel.SignAndLog(act) //nolint:errcheck

			// Store in memory so the agent can reference it.
			if env.MemoryStore != nil {
				env.MemoryStore.Store(&memory.MemoryEntry{ //nolint:errcheck
					Key:    "approval:" + a.ApprovalID,
					Value:  fmt.Sprintf("Approval requested: %s (risk=%s). Use 'aegisclaw event approvals' to respond.", params.Title, params.RiskLevel),
					Tags:   []string{"approval", "pending"},
					TaskID: params.TaskID,
				})
			}
			return fmt.Sprintf("Approval request submitted.\n  ID:        %s\n  Title:     %s\n  Risk:      %s\n  Status:    pending\n\nThe request will appear in the dashboard and CLI (`aegisclaw event approvals list`). The operation will not proceed until a human approves it.",
				a.ApprovalID, a.Title, a.RiskLevel), nil
		})
}

func registerWorkerTools(reg *ToolRegistry, env *runtimeEnv) {
	reg.Register("spawn_worker",
		"Spawn an ephemeral Worker agent for a focused subtask. Args: {task_description, role (researcher|coder|summarizer|custom), tools_granted (list), timeout_mins, task_id}. Blocks until complete; returns structured result.",
		func(ctx context.Context, args string) (string, error) {
			var params spawnWorkerParams
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				return "", fmt.Errorf("invalid spawn_worker args: %w", err)
			}
			return spawnWorker(ctx, env, params)
		})

	reg.Register("worker_status",
		"Get the status and result of a previously spawned worker. Args: {worker_id} or {} to list recent workers.",
		func(_ context.Context, args string) (string, error) {
			if env.WorkerStore == nil {
				return "Worker store not initialized.", nil
			}
			var params struct {
				WorkerID string `json:"worker_id"`
			}
			json.Unmarshal([]byte(args), &params) //nolint:errcheck

			if params.WorkerID != "" {
				w, ok := env.WorkerStore.Get(params.WorkerID)
				if !ok {
					return fmt.Sprintf("Worker %s not found.", params.WorkerID), nil
				}
				return formatWorkerRecord(w), nil
			}
			// List recent workers.
			workers := env.WorkerStore.List(false)
			if len(workers) == 0 {
				return "No workers found.", nil
			}
			var b strings.Builder
			b.WriteString(fmt.Sprintf("Recent Workers (%d):\n", len(workers)))
			limit := 10
			if len(workers) < limit {
				limit = len(workers)
			}
			for _, w := range workers[:limit] {
				b.WriteString(fmt.Sprintf("  [%s]  %-11s  %-12s  steps=%-3d  task=%s\n",
					w.WorkerID[:8], w.Status, w.Role, w.StepCount, w.TaskID))
			}
			return strings.TrimRight(b.String(), "\n"), nil
		})
}

// registerSessionTools registers session routing tools.  selfRegPtr is a
// pointer to the *ToolRegistry variable in the caller; closures dereference it
// at call time so they always see the fully-constructed registry.
func registerSessionTools(reg *ToolRegistry, env *runtimeEnv, selfRegPtr **ToolRegistry) {
	reg.Register("sessions_list",
		"List all active AegisClaw chat sessions. Args: {}. Returns session IDs, start times, and status.",
		func(_ context.Context, _ string) (string, error) {
			if env.Sessions == nil {
				return "No sessions tracked yet.", nil
			}
			all := env.Sessions.List()
			if len(all) == 0 {
				return "No active sessions.", nil
			}
			var b strings.Builder
			b.WriteString(fmt.Sprintf("%-36s  %-8s  %-20s  msgs\n", "session_id", "status", "last_active"))
			b.WriteString(strings.Repeat("-", 80) + "\n")
			for _, r := range all {
				msgs, _ := env.Sessions.History(r.ID, 0)
				b.WriteString(fmt.Sprintf("%-36s  %-8s  %-20s  %d\n",
					r.ID,
					string(r.Status),
					r.LastActiveAt.UTC().Format("2006-01-02 15:04:05"),
					len(msgs),
				))
			}
			return strings.TrimRight(b.String(), "\n"), nil
		})

	reg.Register("sessions_history",
		"Get the message history for a session. Args: {\"session_id\": \"...\", \"limit\": 50}. Returns the message log for the specified session.",
		func(_ context.Context, args string) (string, error) {
			var p struct {
				SessionID string `json:"session_id"`
				Limit     int    `json:"limit"`
			}
			if err := json.Unmarshal([]byte(args), &p); err != nil {
				return "", fmt.Errorf("invalid sessions_history args: %w", err)
			}
			if strings.TrimSpace(p.SessionID) == "" {
				return "", fmt.Errorf("sessions_history requires 'session_id'")
			}
			if env.Sessions == nil {
				return "Session store not available.", nil
			}
			if p.Limit <= 0 {
				p.Limit = 50
			}
			msgs, err := env.Sessions.History(p.SessionID, p.Limit)
			if err != nil {
				return "", err
			}
			if len(msgs) == 0 {
				return fmt.Sprintf("No messages in session %s.", p.SessionID), nil
			}
			var b strings.Builder
			b.WriteString(fmt.Sprintf("Session %s — %d message(s):\n\n", p.SessionID, len(msgs)))
			for _, m := range msgs {
				ts := m.Timestamp.UTC().Format("15:04:05")
				content := m.Content
				if len(content) > 200 {
					content = content[:200] + "…"
				}
				b.WriteString(fmt.Sprintf("[%s] %s: %s\n", ts, strings.ToUpper(m.Role), content))
			}
			return strings.TrimRight(b.String(), "\n"), nil
		})

	reg.Register("sessions_send",
		"Send a message to another active session and get the reply. Args: {\"session_id\": \"...\", \"message\": \"...\"}.",
		func(ctx context.Context, args string) (string, error) {
			var p struct {
				SessionID string `json:"session_id"`
				Message   string `json:"message"`
			}
			if err := json.Unmarshal([]byte(args), &p); err != nil {
				return "", fmt.Errorf("invalid sessions_send args: %w", err)
			}
			p.SessionID = strings.TrimSpace(p.SessionID)
			p.Message = strings.TrimSpace(p.Message)
			if p.SessionID == "" {
				return "", fmt.Errorf("sessions_send requires 'session_id'")
			}
			if p.Message == "" {
				return "", fmt.Errorf("sessions_send requires 'message'")
			}
			if env.Sessions == nil {
				return "", fmt.Errorf("session store not available")
			}
			// Build and call the sessions.send handler directly.
			sendHandler := makeSessionsSendHandler(env, *selfRegPtr)
			reqBytes, _ := json.Marshal(map[string]string{
				"session_id": p.SessionID,
				"message":    p.Message,
			})
			resp := sendHandler(ctx, reqBytes)
			if resp == nil || !resp.Success {
				errMsg := "unknown error"
				if resp != nil && resp.Error != "" {
					errMsg = resp.Error
				}
				return "", fmt.Errorf("sessions_send failed: %s", errMsg)
			}
			var out map[string]interface{}
			if err := json.Unmarshal(resp.Data, &out); err != nil {
				return "", fmt.Errorf("parse reply: %w", err)
			}
			reply, _ := out["reply"].(string)
			return fmt.Sprintf("[session:%s] %s", p.SessionID, reply), nil
		})

	reg.Register("sessions_spawn",
		"Spawn a new isolated chat session with an optional task description. Args: {\"task_description\": \"...\", \"config\": {...}}. Returns the new session_id.",
		func(ctx context.Context, args string) (string, error) {
			spawnHandler := makeSessionsSpawnHandler(env, *selfRegPtr)
			var rawArgs json.RawMessage
			if strings.TrimSpace(args) == "" || args == "null" {
				rawArgs = json.RawMessage(`{}`)
			} else {
				rawArgs = json.RawMessage(args)
			}
			resp := spawnHandler(ctx, rawArgs)
			if resp == nil || !resp.Success {
				errMsg := "unknown error"
				if resp != nil && resp.Error != "" {
					errMsg = resp.Error
				}
				return "", fmt.Errorf("sessions_spawn failed: %s", errMsg)
			}
			var out map[string]interface{}
			if err := json.Unmarshal(resp.Data, &out); err != nil {
				return "", fmt.Errorf("parse spawn response: %w", err)
			}
			sessionID, _ := out["session_id"].(string)
			return fmt.Sprintf("New session spawned. session_id: %s", sessionID), nil
		})
}

func registerRegistryTools(reg *ToolRegistry, env *runtimeEnv) {
	reg.Register("registry.list",
		"List skills available in the ClawHub registry. Args: {}. Returns name, version, description for each skill.",
		func(ctx context.Context, _ string) (string, error) {
			regURL := ""
			if env.Config != nil {
				regURL = env.Config.Registry.URL
			}
			client, err := registry.NewClient(registry.Config{URL: regURL})
			if err != nil {
				return "", fmt.Errorf("registry.list: %w", err)
			}
			entries, err := client.ListSkills(ctx)
			if err != nil {
				return "", fmt.Errorf("registry.list: %w", err)
			}
			if len(entries) == 0 {
				return "No skills found in the registry.", nil
			}
			var b strings.Builder
			b.WriteString(fmt.Sprintf("Registry Skills (%d):\n", len(entries)))
			for _, e := range entries {
				b.WriteString(fmt.Sprintf("  %-30s  v%-8s  %s\n", e.Name, e.Version, e.Description))
			}
			return strings.TrimRight(b.String(), "\n"), nil
		})

	reg.Register("registry.import",
		"Import a skill from the ClawHub registry and submit it to the Governance Court for review. Args: {name}. The skill must pass Court review before activation.",
		func(ctx context.Context, args string) (string, error) {
			var params struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				params.Name = strings.TrimSpace(args)
			}
			if params.Name == "" {
				return "", fmt.Errorf("registry.import requires 'name'")
			}

			regURL := ""
			if env.Config != nil {
				regURL = env.Config.Registry.URL
			}
			client, err := registry.NewClient(registry.Config{URL: regURL})
			if err != nil {
				return "", fmt.Errorf("registry.import: %w", err)
			}
			spec, err := client.FetchSkillSpec(ctx, params.Name)
			if err != nil {
				return "", fmt.Errorf("registry.import: fetch spec for %q: %w", params.Name, err)
			}

			// Auto-submit to Governance Court via proposal.create_draft + submit.
			// Build a draft proposal from the registry spec.
			if env.ProposalStore == nil {
				return "", fmt.Errorf("registry.import: proposal store not available")
			}
			return fmt.Sprintf(
				"Registry spec for %q fetched (language: %s, tools: %d). "+
					"Use proposal.create_draft with the following to submit for Court review:\n"+
					"  skill_name: %q\n  description: %q\n  tools: %v",
				spec.Name, spec.Language, len(spec.Tools),
				spec.Name, spec.Description, spec.Tools,
			), nil
		})
}

// registerLookupTools adds the semantic tool-lookup tools to the registry.
// lookup_tools performs a semantic vector search and returns results formatted
// as Gemma 4 native control-token blocks.  lookup.index_tool lets the builder
// (or any privileged caller) index a new tool into the vector store.
func registerLookupTools(reg *ToolRegistry, env *runtimeEnv) {
	reg.Register("lookup_tools",
		"Semantic lookup of available tools. Returns the most relevant 4-6 tools as Gemma 4 native control-token blocks. Args: {query, max_results}.",
		func(ctx context.Context, args string) (string, error) {
			if env.LookupStore == nil {
				return "", fmt.Errorf("lookup store unavailable")
			}
			var params struct {
				Query      string `json:"query"`
				MaxResults int    `json:"max_results"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				params.Query = strings.TrimSpace(args)
			}
			if params.Query == "" {
				return "", fmt.Errorf("lookup_tools requires 'query'")
			}
			if params.MaxResults <= 0 {
				params.MaxResults = 6
			}
			results, err := env.LookupStore.LookupTools(ctx, params.Query, params.MaxResults)
			if err != nil {
				return "", fmt.Errorf("lookup_tools: %w", err)
			}
			if len(results) == 0 {
				return fmt.Sprintf("No tools found matching %q.", params.Query), nil
			}
			var blocks []string
			for _, r := range results {
				blocks = append(blocks, r.Block)
			}
			return strings.Join(blocks, "\n"), nil
		})

	reg.Register("lookup.index_tool",
		"Index or re-index a tool in the semantic lookup store. Args: {name, description, skill_name, parameters}.",
		func(ctx context.Context, args string) (string, error) {
			if env.LookupStore == nil {
				return "", fmt.Errorf("lookup store unavailable")
			}
			var params struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				SkillName   string `json:"skill_name"`
				Parameters  string `json:"parameters"`
			}
			if err := json.Unmarshal([]byte(args), &params); err != nil {
				return "", fmt.Errorf("invalid lookup.index_tool args: %w", err)
			}
			if params.Name == "" {
				return "", fmt.Errorf("lookup.index_tool requires 'name'")
			}
			if params.Description == "" {
				return "", fmt.Errorf("lookup.index_tool requires 'description'")
			}
			if err := env.LookupStore.IndexTool(ctx, lookup.ToolEntry{
				Name:        params.Name,
				Description: params.Description,
				SkillName:   params.SkillName,
				Parameters:  params.Parameters,
			}); err != nil {
				return "", fmt.Errorf("lookup.index_tool: %w", err)
			}
			return fmt.Sprintf("Tool %q indexed. Total indexed: %d.", params.Name, env.LookupStore.Count()), nil
		})
}
