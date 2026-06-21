package permissions

// Filter is the local enforcement view used by AgentSkillIndex.
type Filter struct {
	AllowedTools     map[string]bool
	VisibleTools     map[string]bool
	RequestableTools map[string]bool
	CanDiscover      bool
	Enforce          bool // false = backward-compat allow-all (tests/bootstrap)
}

// BuildFilter constructs the local filter for a subject from durable state + known capabilities.
func BuildFilter(state *State, subjectID string, allCapabilities []string) Filter {
	if state == nil {
		return Filter{Enforce: false}
	}

	allowed := make(map[string]bool)
	for _, g := range state.Grants {
		if SubjectMatches(subjectID, g.Subject) {
			allowed[g.Capability] = true
		}
	}

	visible := make(map[string]bool)
	requestable := make(map[string]bool)

	visByCap := resolveVisibility(state, subjectID)

	for _, cap := range allCapabilities {
		level := visByCap[cap]
		if level == "" {
			level = defaultVisibility(cap)
		}

		switch level {
		case VisibilityHidden:
			continue
		case VisibilityGrantedOnly:
			if allowed[cap] {
				visible[cap] = true
			}
		case VisibilityRequestable:
			visible[cap] = true
			if !allowed[cap] {
				requestable[cap] = true
			}
		case VisibilityPublic:
			visible[cap] = true
		default:
			if allowed[cap] {
				visible[cap] = true
			}
		}
	}

	// Granted tools are always visible for discovery (tool.list safe path).
	for cap := range allowed {
		visible[cap] = true
	}

	canDiscover := allowed["tool.registry.discover"]

	return Filter{
		AllowedTools:     allowed,
		VisibleTools:     visible,
		RequestableTools: requestable,
		CanDiscover:      canDiscover,
		Enforce:          len(state.Grants) > 0 || len(state.Visibility) > 0,
	}
}

func resolveVisibility(state *State, subjectID string) map[string]VisibilityLevel {
	out := make(map[string]VisibilityLevel)
	for _, r := range state.Visibility {
		if SubjectMatches(subjectID, r.Subject) {
			out[r.Capability] = r.Level
		}
	}
	return out
}

// defaultVisibility returns paranoid defaults for high-sensitivity capabilities.
func defaultVisibility(cap string) VisibilityLevel {
	switch {
	case cap == "tool.registry.discover":
		return VisibilityGrantedOnly
	case stringsHasPrefixAny(cap, []string{"court.", "secrets.", "permission.grant", "visibility.set"}):
		return VisibilityHidden
	case stringsHasPrefixAny(cap, []string{"channel.", "memory.", "llm.", "tool.list", "tool.search"}):
		return VisibilityPublic
	default:
		return VisibilityGrantedOnly
	}
}

func stringsHasPrefixAny(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if len(s) >= len(p) && s[:len(p)] == p {
			return true
		}
	}
	return false
}

// BuildSnapshot creates the wire format for Hub → microVM distribution.
func BuildSnapshot(state *State, subjectID string, allCapabilities []string) Snapshot {
	f := BuildFilter(state, subjectID, allCapabilities)
	return Snapshot{
		Subject:          subjectID,
		AllowedTools:     f.AllowedTools,
		VisibleTools:     f.VisibleTools,
		RequestableTools: f.RequestableTools,
		CanDiscover:      f.CanDiscover,
		Version:          state.Version,
		Timestamp:        NowRFC3339(),
	}
}

// HasGrant checks whether subject holds an explicit grant for capability.
func HasGrant(state *State, subjectID, capability string) bool {
	if state == nil {
		return false
	}
	for _, g := range state.Grants {
		if g.Capability == capability && SubjectMatches(subjectID, g.Subject) {
			return true
		}
	}
	return false
}

// IsVisibleForDiscovery reports whether capability may appear in discovery for subject.
func IsVisibleForDiscovery(state *State, subjectID, capability string, allCapabilities []string) bool {
	f := BuildFilter(state, subjectID, allCapabilities)
	if !f.Enforce {
		return true
	}
	return f.VisibleTools[capability]
}
