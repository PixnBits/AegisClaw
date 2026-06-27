package permissions

import (
	"fmt"
	"strings"
)

// GrantCapability adds or updates a grant. Returns error if subject tries self-grant via microVM source.
func GrantCapability(state *State, subject, capability, grantedBy, reason string) error {
	if isMicroVMSource(grantedBy) && grantedBy == subject {
		return fmt.Errorf("ERR_PERMISSION_DENIED: self-grant forbidden")
	}
	if state == nil {
		return fmt.Errorf("ERR_PERMISSION_DENIED: nil permission state")
	}
	// Remove existing grant for same subject pattern + capability
	filtered := state.Grants[:0]
	for _, g := range state.Grants {
		if g.Subject == subject && g.Capability == capability {
			continue
		}
		filtered = append(filtered, g)
	}
	state.Grants = append(filtered, Grant{
		Subject:    subject,
		Capability: capability,
		GrantedBy:  grantedBy,
		GrantedAt:  NowRFC3339(),
		Reason:     reason,
	})
	state.Version++
	return nil
}

// RevokeCapability removes a grant.
func RevokeCapability(state *State, subject, capability string) bool {
	if state == nil {
		return false
	}
	found := false
	filtered := state.Grants[:0]
	for _, g := range state.Grants {
		if g.Subject == subject && g.Capability == capability {
			found = true
			continue
		}
		filtered = append(filtered, g)
	}
	if found {
		state.Grants = filtered
		state.Version++
	}
	return found
}

// SetVisibility upserts a visibility rule.
func SetVisibility(state *State, subject, capability string, level VisibilityLevel, setBy, reason string) {
	if state == nil {
		return
	}
	filtered := state.Visibility[:0]
	for _, r := range state.Visibility {
		if r.Subject == subject && r.Capability == capability {
			continue
		}
		filtered = append(filtered, r)
	}
	state.Visibility = append(filtered, VisibilityRule{
		Subject:    subject,
		Capability: capability,
		Level:      level,
		Reason:     reason,
		SetBy:      setBy,
		SetAt:      NowRFC3339(),
	})
	state.Version++
}

// RecordRequest appends a permission request for denied tool use.
func RecordRequest(state *State, subject, capability, context string) (Request, error) {
	if state == nil {
		return Request{}, fmt.Errorf("ERR_PERMISSION_DENIED: nil permission state")
	}
	id := fmt.Sprintf("perm-req-%d", len(state.Requests)+1)
	req := Request{
		ID:         id,
		Subject:    subject,
		Capability: capability,
		Context:    context,
		Timestamp:  NowRFC3339(),
		Status:     "pending",
	}
	state.Requests = append(state.Requests, req)
	state.Version++
	return req, nil
}

// ListGrantsForSubject returns grants applicable to subjectID.
func ListGrantsForSubject(state *State, subjectID string) []Grant {
	if state == nil {
		return nil
	}
	var out []Grant
	for _, g := range state.Grants {
		if SubjectMatches(subjectID, g.Subject) {
			out = append(out, g)
		}
	}
	return out
}

// ListRequestsForSubject returns permission requests for a subject.
func ListRequestsForSubject(state *State, subjectID string) []Request {
	if state == nil {
		return nil
	}
	var out []Request
	for _, r := range state.Requests {
		if r.Subject == subjectID {
			out = append(out, r)
		}
	}
	return out
}

// IsMicroVMSourcePublic reports whether source is a microVM component (cannot self-grant).
func IsMicroVMSourcePublic(source string) bool {
	return isMicroVMSource(source)
}

func isMicroVMSource(source string) bool {
	prefixes := []string{"agent", "project-manager", "coder", "tester", "builder", "memory", "court-persona"}
	for _, p := range prefixes {
		if source == p || (len(source) > len(p) && source[:len(p)+1] == p+"-") {
			return true
		}
	}
	return false
}

// AllowsCisoDelegation returns whether a CISO persona source is permitted to act on
// grants/visibility when the delegation flag is enabled (opt-in only).
func AllowsCisoDelegation(source string, enabled bool) bool {
	if !enabled {
		return false
	}
	return strings.HasPrefix(source, "court-persona-ciso") || source == "ciso" || strings.HasPrefix(source, "ciso-")
}

// DefaultBootstrap returns minimal bootstrap grants + visibility for pre-alpha startup.
func DefaultBootstrap() *State {
	s := NewState()
	// Project Manager: channel + LLM + memory + safe discovery + turn anchor tools
	for _, cap := range []string{
		"channel.create", "channel.list", "channel.get", "channel.join", "channel.post",
		"channel.get_relevant_since", "channel.get_messages",
		"llm.call", "memory.store", "memory.query", "tool.list", "tool.search",
	} {
		_ = GrantCapability(s, "project-manager*", cap, "bootstrap", "minimal PM bootstrap")
	}
	// Generic agents: memory + LLM + channel read/post + turn anchor tools
	for _, cap := range []string{
		"channel.list", "channel.get", "channel.post",
		"channel.get_relevant_since", "channel.get_messages",
		"llm.call", "memory.store", "memory.query", "tool.list", "tool.search",
	} {
		_ = GrantCapability(s, "agent*", cap, "bootstrap", "minimal agent bootstrap")
	}
	// Coder persona: narrower write surface + turn anchor tools
	for _, cap := range []string{
		"channel.list", "channel.get", "channel.post",
		"channel.get_relevant_since", "channel.get_messages",
		"llm.call", "memory.store", "tool.list", "tool.search",
	} {
		_ = GrantCapability(s, "coder*", cap, "bootstrap", "minimal coder bootstrap")
	}
	// Tester / researcher on-demand roles (turn-based channel propagation)
	for _, pattern := range []string{"tester*", "researcher*", "architect*", "ciso*"} {
		for _, cap := range []string{
			"channel.list", "channel.get", "channel.post",
			"channel.get_relevant_since", "channel.get_messages",
			"llm.call", "tool.list", "tool.search",
		} {
			_ = GrantCapability(s, pattern, cap, "bootstrap", "on-demand role bootstrap")
		}
	}
	// Court personas: channel + LLM for governance participation (incl. turn anchor tools)
	for _, cap := range []string{
		"channel.list", "channel.get", "channel.post",
		"channel.get_relevant_since", "channel.get_messages",
		"llm.call", "tool.list", "tool.search",
	} {
		_ = GrantCapability(s, "court-persona*", cap, "bootstrap", "court persona bootstrap")
	}
	// Hide high-privilege from non-Court personas
	for _, cap := range []string{
		"court.review", "permission.grant", "permission.revoke", "visibility.set",
		"secrets.push", "proposal.create",
	} {
		SetVisibility(s, "agent*", cap, VisibilityHidden, "bootstrap", "anti-fingerprinting default")
		SetVisibility(s, "coder*", cap, VisibilityHidden, "bootstrap", "anti-fingerprinting default")
		SetVisibility(s, "project-manager*", cap, VisibilityHidden, "bootstrap", "anti-fingerprinting default")
	}
	// tool.registry.discover is grantable + hidden by default
	SetVisibility(s, "*", "tool.registry.discover", VisibilityGrantedOnly, "bootstrap", "broad discovery is grant-gated")
	return s
}
