// Package skills provides the fast, stdlib-only local semantic skill/tool index
// that every Agent Runtime VM carries.
//
// This is the 7.3 component: always available inside the 6-step loop and for
// direct "tool.list" / "tool.search" commands from the Hub/Portal/CLI.
// No external vector DB or heavy dependencies.
//
// SPEC REFERENCES (cited in every file and commit in this package):
//   - docs/specs/agent-runtime.md §Responsibilities + §Key Interfaces
//     ("Call skills/tools exclusively through AegisHub", "agent.loop.step", local tool awareness in every turn)
//   - docs/prd/runtime-architecture.md (Agent Runtime VMs must have fast local discovery)
//   - docs/no-stubs-plan/phase-1.md 1.1b (move the proven 7.3 index into internal/agent/skills as part of real runtime)
//   - security-model.md (the index helps enforce "only use tools from the available local index" — least privilege)
//   - docs/specs/permissions-model.md (dual grant + visibility filtering for least-privilege discovery and use)
//
// Design preserved exactly from the prior working implementation (cmd/agent/main.go:556-873):
//   - Simple in-memory index with Jaccard + substring + Levenshtein + TF boosts
//   - Seeded with realistic examples (discord_monitor, web_research) that are replaced at runtime
//     by real data from Store/Hub via "skill.register" / "index.update" events.
//   - Future: invalidation via EventBus (7.2) — the index is already designed for it.
//
// The types Skill, Tool, SearchResult, and AgentSkillIndex (with all methods) are exported
// with identical names/signatures so existing call sites in the thin main and special command
// handlers continue to work with a simple import alias during the 1.1b refactor.

package skills

import (
	"errors"
	"strings"
)

// ErrPermissionDenied is returned when a tool invocation lacks a grant.
var ErrPermissionDenied = errors.New("ERR_PERMISSION_DENIED")

// Skill represents a registered skill (higher level than a single tool).
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// Tool is a concrete invocable action belonging to a Skill.
type Tool struct {
	Name        string `json:"name"`
	Description string   `json:"description"`
	SkillID     string `json:"skill_id"`
}

// SearchResult is a scored match from SearchTools.
type SearchResult struct {
	Tool        Tool    `json:"tool"`
	SkillName   string  `json:"skill_name"`
	Score       float64 `json:"score"`
	Description string  `json:"description"`
}

// PermissionFilter controls which tools are visible in discovery and which
// may actually be invoked. It is populated from the signed permission snapshot
// received from AegisHub (which in turn comes from Store).
//
// - AllowedTools: capabilities the subject may invoke.
// - VisibleTools: capabilities that may appear in discovery (includes granted + requestable/public).
// - RequestableTools: visible but not yet granted (agent can see and request access).
// - Enforce: when false, allow-all for backward compat (tests without permission state).
// - CanDiscoverRegistry: subject holds tool.registry.discover grant.
type PermissionFilter struct {
	AllowedTools          map[string]bool
	VisibleTools          map[string]bool
	RequestableTools      map[string]bool
	Enforce               bool
	CanDiscoverRegistry   bool
}

// AgentSkillIndex is the fast local index every agent runtime carries (7.3).
// It is injected into every step of the 6-step loop so that reasoning and
// tool invocation are always constrained to what the agent is actually allowed
// to use right now (security + spec requirement).
//
// With permissions enabled, it also enforces the PermissionFilter so that
// agents can only discover and use tools they have been explicitly granted
// (and that are visible to them).
type AgentSkillIndex struct {
	skills     []Skill
	tools      []Tool
	permFilter PermissionFilter
}

// NewAgentSkillIndex creates a fresh index seeded with a small set of realistic
// example skills/tools. In production these are replaced/added via Hub messages
// ("skill.register", "skill.deployed", "index.update").
func NewAgentSkillIndex() *AgentSkillIndex {
	idx := &AgentSkillIndex{}
	// Seed a few realistic examples so the index is immediately useful in dev.
	// In real operation these come from Store/Hub on startup or via events.
	idx.AddSkill(Skill{
		ID: "discord_monitor", Name: "discord_monitor",
		Description: "Monitor Discord channels and send messages. Supports sending alerts and summaries.",
		Version: "1.2.0", Tags: []string{"discord", "social", "notification"},
	})
	idx.AddTool(Tool{Name: "discord_monitor.send_message", Description: "Send a message to a specific Discord channel", SkillID: "discord_monitor"})
	idx.AddTool(Tool{Name: "discord_monitor.get_recent", Description: "Retrieve recent messages from a channel", SkillID: "discord_monitor"})

	idx.AddSkill(Skill{
		ID: "web_research", Name: "web_research",
		Description: "Search the web, fetch pages, and summarize information from public sources.",
		Version: "0.9.1", Tags: []string{"web", "research", "search"},
	})
	idx.AddTool(Tool{Name: "web_research.search", Description: "Perform a web search for a query", SkillID: "web_research"})
	idx.AddTool(Tool{Name: "web_research.summarize_url", Description: "Fetch and summarize the content of a URL", SkillID: "web_research"})

	return idx
}

func (idx *AgentSkillIndex) AddSkill(s Skill) {
	idx.skills = append(idx.skills, s)
}

func (idx *AgentSkillIndex) AddTool(t Tool) {
	idx.tools = append(idx.tools, t)
}

// SetPermissionFilter installs (or replaces) the permission + visibility filter
// for this index. It should be called when the agent receives its signed
// permission snapshot from AegisHub during startup or after a grant/visibility
// change.
func (idx *AgentSkillIndex) SetPermissionFilter(f PermissionFilter) {
	idx.permFilter = f
}

// isToolAllowed returns true if the tool may be used/invoked.
func (idx *AgentSkillIndex) isToolAllowed(name string) bool {
	if !idx.permFilter.Enforce {
		return true
	}
	return idx.permFilter.AllowedTools[name]
}

// isToolDiscoverable returns true if the tool should appear in tool.list/search.
// Per spec: granted tools + publicly discoverable/requestable visible tools.
func (idx *AgentSkillIndex) isToolDiscoverable(name string) bool {
	if !idx.permFilter.Enforce {
		return true
	}
	return idx.permFilter.VisibleTools[name]
}

// CheckToolInvoke verifies the subject may invoke a capability; returns ErrPermissionDenied if not.
func (idx *AgentSkillIndex) CheckToolInvoke(name string) error {
	if idx.isToolAllowed(name) {
		return nil
	}
	return ErrPermissionDenied
}

// ListSkills returns all known skills (exact, fast path).
// Note: skills are currently not filtered by permission (only tools are).
// This matches the current spec focus on tool-level grants.
func (idx *AgentSkillIndex) ListSkills() []Skill {
	return append([]Skill(nil), idx.skills...) // copy
}

// ListTools returns tools the agent may invoke (granted only). Used for prompt injection.
func (idx *AgentSkillIndex) ListTools() []Tool {
	filtered := make([]Tool, 0, len(idx.tools))
	for _, t := range idx.tools {
		if idx.isToolAllowed(t.Name) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// ListDiscoverableTools returns granted + requestable/public visible tools for tool.list.
func (idx *AgentSkillIndex) ListDiscoverableTools() []Tool {
	filtered := make([]Tool, 0, len(idx.tools))
	for _, t := range idx.tools {
		if idx.isToolDiscoverable(t.Name) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// SearchTools performs a simple stdlib-only semantic-ish search.
// Uses token overlap (Jaccard-style) + bonus for name/description substring matches.
// Returns top results sorted by score (highest first).
//
// Results are filtered to discoverable tools (granted + requestable/public visible).
func (idx *AgentSkillIndex) SearchTools(query string, limit int) []SearchResult {
	if limit <= 0 {
		limit = 10
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}

	qTokens := tokenize(q)
	results := make([]SearchResult, 0, len(idx.tools))

	for _, t := range idx.tools {
		if !idx.isToolDiscoverable(t.Name) {
			continue
		}

		skillName := t.SkillID
		for _, s := range idx.skills {
			if s.ID == t.SkillID {
				skillName = s.Name
				break
			}
		}

		text := strings.ToLower(t.Name + " " + t.Description + " " + skillName)
		tTokens := tokenize(text)

		score := jaccardScore(qTokens, tTokens)

		// Bonus for direct substring matches (makes it feel more "semantic" without embeddings)
		if strings.Contains(text, q) {
			score += 0.25
		}
		if strings.Contains(t.Name, q) {
			score += 0.15
		}

		// Cheap Levenshtein boost for very short queries (single concept match)
		if len(qTokens) <= 3 {
			for _, qt := range qTokens {
				for _, tt := range tTokens {
					if levenshtein(qt, tt) <= 2 { // allow 1-2 char typos
						score += 0.1
						break
					}
				}
			}
		}

		// Light TF boost: reward tools whose description contains the query terms multiple times
		tfBoost := 0.0
		for _, qt := range qTokens {
			count := strings.Count(text, qt)
			if count > 1 {
				tfBoost += 0.05 * float64(count-1)
			}
		}
		score += tfBoost

		if score > 0.05 { // filter out very weak matches
			results = append(results, SearchResult{
				Tool:        t,
				SkillName:   skillName,
				Score:       score,
				Description: t.Description,
			})
		}
	}

	// Sort by score desc
	sortSearchResults(results)

	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

func tokenize(s string) []string {
	// Very simple word tokenizer (good enough for this phase)
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	seen := make(map[string]bool)
	var out []string
	for _, f := range fields {
		if len(f) > 2 && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

func jaccardScore(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := make(map[string]bool, len(a))
	for _, x := range a {
		setA[x] = true
	}
	inter := 0
	for _, x := range b {
		if setA[x] {
			inter++
		}
	}
	union := len(setA) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func sortSearchResults(res []SearchResult) {
	// Simple insertion sort (tiny data, no need for sort package dependency concerns)
	for i := 1; i < len(res); i++ {
		j := i
		for j > 0 && res[j].Score > res[j-1].Score {
			res[j], res[j-1] = res[j-1], res[j]
			j--
		}
	}
}

// levenshtein is a tiny stdlib-only distance for close-match boosting in search.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 0; i < len(a); i++ {
		curr := make([]int, len(b)+1)
		curr[0] = i + 1
		for j := 0; j < len(b); j++ {
			cost := 0
			if a[i] != b[j] {
				cost = 1
			}
			curr[j+1] = min(
				curr[j]+1,      // insertion
				prev[j+1]+1,    // deletion
				prev[j]+cost,   // substitution
			)
		}
		prev = curr
	}
	return prev[len(b)]
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// FormatAvailableTools returns a compact string of the local skill/tool index
// for injection into LLM prompts (keeps context reasonable).
// This is the exact helper used by the 6-step prompts (7.4 / 7.6 integration
// with custom TOOLS.md and SKILLS.md from the workspace loader).
//
// Only granted (invokable) tools are included in LLM prompts.
func FormatAvailableTools(idx *AgentSkillIndex, loadedWorkspace interface {
	GetSOUL() string
	GetAGENTS() string
	GetTOOLS() string
	GetSKILLS() string
}) string {
	if idx == nil {
		return "(no local tool index available)"
	}
	var b strings.Builder
	b.WriteString("Skills: ")
	for i, s := range idx.skills {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(s.Name)
		b.WriteString(" (")
		b.WriteString(s.Description)
		b.WriteString(")")
	}
	b.WriteString(". Tools: ")
	visibleTools := idx.ListTools()
	for i, t := range visibleTools {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(t.Name)
	}

	// 7.4/7.6 integration: Inject custom tool descriptions from workspace if present
	if loadedWorkspace != nil {
		if t := loadedWorkspace.GetTOOLS(); t != "" {
			b.WriteString(". Custom tool guidance: ")
			b.WriteString(t)
		}
		if s := loadedWorkspace.GetSKILLS(); s != "" {
			b.WriteString(". Additional skills context: ")
			b.WriteString(s)
		}
	}

	return b.String()
}

// HandleToolCommand is the dispatcher for the fast-path "tool.list" and "tool.search"
// commands that bypass the full 6-step LLM loop (7.3 requirement).
//
// "tool.list" returns discoverable tools (granted + requestable/public visible).
func HandleToolCommand(cmd string, payload interface{}, idx *AgentSkillIndex) interface{} {
	switch cmd {
	case "tool.list":
		return map[string]interface{}{
			"skills": idx.ListSkills(),
			"tools":  idx.ListDiscoverableTools(),
		}
	case "tool.registry.discover":
		if idx.permFilter.Enforce && !idx.permFilter.CanDiscoverRegistry {
			return map[string]string{"error": "ERR_PERMISSION_DENIED"}
		}
		return map[string]interface{}{
			"tools": idx.ListDiscoverableTools(),
		}
	case "tool.search":
		q := ""
		if p, ok := payload.(map[string]interface{}); ok {
			if qq, ok := p["query"].(string); ok {
				q = qq
			}
		}
		return map[string]interface{}{
			"query":   q,
			"results": idx.SearchTools(q, 8),
		}
	}
	return map[string]string{"error": "unknown tool command"}
}

// GetString is a tiny helper for safe map[string]interface{} access (used by dynamic index updates).
func GetString(m map[string]interface{}, key, def string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}
