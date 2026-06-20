package collab

import "strings"

// NormalizeChannelLLMReply interprets raw LLM output for channel.post.
// Returns skip=true when the agent should not post. Otherwise returns cleaned text
// with standalone NO_REPLY control lines removed (models often append NO_REPLY after prose).
func NormalizeChannelLLMReply(raw string) (content string, skip bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", true
	}
	if strings.EqualFold(s, "NO_REPLY") {
		return "", true
	}
	firstLine, _, _ := strings.Cut(s, "\n")
	if strings.EqualFold(strings.TrimSpace(firstLine), "NO_REPLY") {
		return "", true
	}
	for {
		s = strings.TrimSpace(s)
		lastNL := strings.LastIndex(s, "\n")
		lastLine := s
		if lastNL >= 0 {
			lastLine = s[lastNL+1:]
		}
		if !strings.EqualFold(strings.TrimSpace(lastLine), "NO_REPLY") {
			break
		}
		if lastNL < 0 {
			return "", true
		}
		s = strings.TrimSpace(s[:lastNL])
	}
	if s == "" {
		return "", true
	}
	return s, false
}
