package collab

import "strings"

// NormalizeMemberRole maps portal display labels and casual aliases to hub role ids
// used for ensure.role, turn delivery, and channel membership.
func NormalizeMemberRole(role string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		return role
	}
	if strings.HasPrefix(role, "court-persona-") || strings.HasPrefix(role, "user:") || role == "user" {
		return role
	}
	if role == "project-manager" || strings.HasPrefix(role, "project-manager-") {
		return "project-manager"
	}

	lower := strings.ToLower(role)
	switch lower {
	case "coder", "senior-coder", "senior coder":
		return "coder"
	case "tester":
		return "tester"
	case "ciso":
		return "ciso"
	case "architect", "system architect":
		return "architect"
	case "researcher":
		return "researcher"
	case "project manager", "pm":
		return "project-manager"
	}

	for _, id := range MainChannelRoster {
		if DisplayName(id) == role {
			return id
		}
	}
	if slug := PersonaSlugFromSource(role); slug != "" {
		return "court-persona-" + slug
	}
	// Portal invite form accepts "user:alice"; bare names are human placeholders.
	if !strings.Contains(role, "-") {
		slug := strings.ToLower(strings.ReplaceAll(role, " ", "-"))
		if slug != "" && slug != "user" {
			return "user:" + slug
		}
	}
	return role
}