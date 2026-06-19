package collab

import "strings"

func normalizeCollabSourceID(sourceID string) string {
	if sourceID == "project-manager" || strings.HasPrefix(sourceID, "project-manager-") {
		return "project-manager"
	}
	return sourceID
}

// MainChannelRoster is the default solo-user channel membership (PM + 7 Court personas).
var MainChannelRoster = []string{
	"project-manager",
	"court-persona-ciso",
	"court-persona-security-architect",
	"court-persona-architect",
	"court-persona-senior-coder",
	"court-persona-tester",
	"court-persona-efficiency",
	"court-persona-user-advocate",
}

// PersonaSlugFromSource returns the short persona slug (e.g. "ciso") from a hub source id.
func PersonaSlugFromSource(sourceID string) string {
	const prefix = "court-persona-"
	if strings.HasPrefix(sourceID, prefix) {
		return strings.TrimPrefix(sourceID, prefix)
	}
	return ""
}

// DisplayName returns a human-readable role name for intro responses and E2E assertions.
func DisplayName(sourceID string) string {
	sourceID = normalizeCollabSourceID(sourceID)
	switch sourceID {
	case "project-manager":
		return "Project Manager"
	case "court-persona-ciso":
		return "Chief Information Security Officer (CISO)"
	case "court-persona-security-architect":
		return "Security Architect"
	case "court-persona-architect":
		return "System Architect"
	case "court-persona-senior-coder":
		return "Senior Coder"
	case "court-persona-tester":
		return "Tester"
	case "court-persona-efficiency":
		return "Efficiency Expert"
	case "court-persona-user-advocate":
		return "User Advocate"
	default:
		if slug := PersonaSlugFromSource(sourceID); slug != "" {
			return strings.ReplaceAll(slug, "-", " ")
		}
		return sourceID
	}
}

// AgentRoleLabel returns a human-readable SDLC role from an on-demand agent VM id (e.g. coder-plan-demo).
func AgentRoleLabel(sourceID string) string {
	lower := strings.ToLower(sourceID)
	switch {
	case strings.HasPrefix(lower, "coder"):
		return "Senior Coder"
	case strings.HasPrefix(lower, "tester"):
		return "Tester"
	case strings.HasPrefix(lower, "architect"):
		return "Architect"
	default:
		return "Agent"
	}
}

// AgentFallbackIntro produces a deterministic channel reply for generic on-demand agent VMs.
func AgentFallbackIntro(sourceID string) string {
	role := AgentRoleLabel(sourceID)
	switch role {
	case "Senior Coder":
		return "I'm the Senior Coder on this channel. I focus on code quality, implementation standards, and technical feasibility."
	case "Tester":
		return "I'm the Tester on this channel. I focus on test strategy, coverage, edge cases, and reliability."
	default:
		return "I'm " + role + " on this channel. I contribute my specialized perspective to planning and implementation."
	}
}

// FallbackIntro produces deterministic intro text for unit tests and documentation only.
// Production agents must not auto-post this text (see cmd/court-persona, cmd/project-manager).
func FallbackIntro(sourceID string) string {
	sourceID = normalizeCollabSourceID(sourceID)
	name := DisplayName(sourceID)
	switch sourceID {
	case "project-manager":
		return "I'm the Project Manager. I coordinate goals across channels, produce actionable plans, ensure SDLC roles, and monitor progress until Court review when needed."
	case "court-persona-ciso":
		return "I'm the Chief Information Security Officer (CISO). I evaluate proposals for security risks, compliance, and business impact before the Court decides."
	case "court-persona-security-architect":
		return "I'm the Security Architect. I assess technical security design, attack surface, sandbox boundaries, and privilege escalation risks."
	case "court-persona-architect":
		return "I'm the System Architect. I review system design, modularity, maintainability, and long-term architectural implications."
	case "court-persona-senior-coder":
		return "I'm the Senior Coder. I evaluate code quality, readability, implementation standards, and correctness."
	case "court-persona-tester":
		return "I'm the Tester. I assess testing strategy, coverage, edge cases, and reliability of proposed changes."
	case "court-persona-efficiency":
		return "I'm the Efficiency Expert. I review performance, resource usage, cost, and latency trade-offs."
	case "court-persona-user-advocate":
		return "I'm the User Advocate. I consider usability, UX, human impact, and accessibility for end users."
	default:
		return "I'm " + name + ". I participate in channel collaboration and contribute my specialized perspective to reviews and planning."
	}
}

// AssertionKeywords returns substrings we expect in an intro post for E2E validation.
func AssertionKeywords(sourceID string) []string {
	switch sourceID {
	case "project-manager":
		return []string{"project manager", "coordinate", "plan"}
	case "court-persona-ciso":
		return []string{"ciso", "security"}
	case "court-persona-security-architect":
		return []string{"security architect", "security"}
	case "court-persona-architect":
		return []string{"architect", "design"}
	case "court-persona-senior-coder":
		return []string{"coder", "code"}
	case "court-persona-tester":
		return []string{"tester", "test"}
	case "court-persona-efficiency":
		return []string{"efficiency", "performance"}
	case "court-persona-user-advocate":
		return []string{"user advocate", "user"}
	default:
		name := strings.ToLower(DisplayName(sourceID))
		return []string{name}
	}
}
