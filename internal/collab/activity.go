package collab

import "strings"

// ResponseReason explains why an agent chose to respond or stay quiet.
type ResponseReason string

const (
	ReasonSelfPost     ResponseReason = "self_post"
	ReasonDelivered    ResponseReason = "delivered" // activity delivered; agent decides whether/how to reply
	ReasonBroadcast    ResponseReason = "broadcast" // hint for agent prompts only (not a system gate)
	ReasonMention      ResponseReason = "mention"     // hint for agent prompts only (not a system gate)
	ReasonUserMonitor  ResponseReason = "user_monitor"
	ReasonNoMatch      ResponseReason = "no_match"
)

// IsSelfPost reports whether the activity was authored by the receiving member.
func IsSelfPost(memberSourceID, from string) bool {
	from = strings.TrimSpace(from)
	if from == "" {
		return false
	}
	if from == memberSourceID {
		return true
	}
	if memberSourceID == "project-manager" || strings.HasPrefix(memberSourceID, "project-manager-") {
		return strings.HasPrefix(from, "project-manager")
	}
	return false
}

// PayloadContentString extracts channel message text from channel.activity payloads.
// Store may persist content as a string or nested map after portal/relay paths.
func PayloadContentString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	if m, ok := v.(map[string]interface{}); ok {
		if s, ok := m["content"].(string); ok {
			return s
		}
	}
	return ""
}

// IsCorruptedMapString reports Go fmt %v map dumps mistakenly posted as channel content.
func IsCorruptedMapString(s string) bool {
	trim := strings.TrimSpace(s)
	return strings.HasPrefix(trim, "map[") && strings.Contains(trim, "channel_id:")
}

// IsHumanPoster returns true for user-facing post sources (CLI, portal, etc.).
func IsHumanPoster(from string) bool {
	switch strings.ToLower(strings.TrimSpace(from)) {
	case "user", "cli", "web-portal", "portal", "operator":
		return true
	default:
		return false
	}
}

// IsBroadcast detects messages addressed to everyone in the channel.
func IsBroadcast(content string) bool {
	lower := strings.ToLower(content)
	for _, phrase := range []string{
		"everyone",
		"all of you",
		"each of you",
		"can everyone",
		"can you all",
		"you all",
		"all agents",
		"whole team",
		"introduce yourself",
		"tell me your name",
		"who are you",
		"what do you do",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// IsMentioned reports whether content @mentions or names this member.
func IsMentioned(memberSourceID, content string) bool {
	lower := strings.ToLower(content)
	candidates := []string{
		"@" + strings.ToLower(memberSourceID),
		strings.ToLower(memberSourceID),
	}
	if slug := PersonaSlugFromSource(memberSourceID); slug != "" {
		candidates = append(candidates,
			"@"+slug,
			"@"+strings.ReplaceAll(slug, "-", " "),
			"@"+strings.ReplaceAll(slug, "-", ""),
		)
	}
	name := strings.ToLower(DisplayName(memberSourceID))
	if name != "" {
		candidates = append(candidates, "@"+name, name)
	}
	if memberSourceID == "project-manager" || strings.HasPrefix(memberSourceID, "project-manager") {
		candidates = append(candidates, "@projectmanager", "@project-manager", "@project manager", "project manager")
	}
	for _, c := range candidates {
		if c != "" && strings.Contains(lower, c) {
			return true
		}
	}
	return false
}

// ShouldRespondToActivity reports whether channel.activity should be presented to a member.
// The system only skips self-posts; each agent decides locally whether to post a reply.
func ShouldRespondToActivity(memberSourceID, from, content string) (bool, ResponseReason) {
	if IsSelfPost(memberSourceID, from) {
		return false, ReasonSelfPost
	}
	return true, ReasonDelivered
}

// ActivityHints returns non-blocking hints agents may use when crafting a reply (LLM prompt, fallback).
func ActivityHints(memberSourceID, content string) (broadcast, mentioned bool) {
	return IsBroadcast(content), IsMentioned(memberSourceID, content)
}

// ShouldPMMonitor returns true when PM should post a lightweight monitoring note (not a full plan).
func ShouldPMMonitor(from, content string) bool {
	if IsHumanPoster(from) && !IsBroadcast(content) {
		return true
	}
	if IsMentioned("project-manager", content) {
		return true
	}
	return false
}
