package collab

import (
	"regexp"
	"strings"
)

var thinkTagRe = regexp.MustCompile(`(?is)<think>.*?</think>`)

// StripThinkTags removes model chain-of-thought wrappers that must never be posted.
func StripThinkTags(s string) string {
	return strings.TrimSpace(thinkTagRe.ReplaceAllString(s, ""))
}

func normalizeDecisionLine(line string) string {
	s := strings.TrimSpace(line)
	s = strings.Trim(s, "`*_ \t")
	s = strings.TrimSpace(s)
	upper := strings.ToUpper(s)
	upper = strings.TrimPrefix(upper, "DECISION:")
	upper = strings.TrimPrefix(upper, "ACTION:")
	upper = strings.TrimSpace(upper)
	upper = strings.TrimRight(upper, ".!:")
	upper = strings.TrimSpace(upper)
	return upper
}

func isPassToken(tok string) bool {
	switch tok {
	case "PASS", "NO_REPLY", "NOREPLY", "SILENT", "SKIP":
		return true
	}
	if strings.HasPrefix(tok, "PASS:") || strings.HasPrefix(tok, "PASS ") {
		return true
	}
	if strings.HasPrefix(tok, "NO_REPLY:") || strings.HasPrefix(tok, "NO_REPLY ") {
		return true
	}
	return false
}

func isSpeakToken(tok string) bool {
	switch tok {
	case "SPEAK", "REPLY":
		return true
	}
	if strings.HasPrefix(tok, "SPEAK:") || strings.HasPrefix(tok, "SPEAK ") {
		return true
	}
	if strings.HasPrefix(tok, "REPLY:") || strings.HasPrefix(tok, "REPLY ") {
		return true
	}
	return false
}

func inlineAfterSpeak(firstLine, rest string) string {
	upper := strings.ToUpper(strings.TrimSpace(firstLine))
	for _, prefix := range []string{"SPEAK:", "SPEAK ", "REPLY:", "REPLY "} {
		if strings.HasPrefix(upper, prefix) {
			// Slice the original line by prefix length (same rune count for these ASCII prefixes).
			body := strings.TrimSpace(firstLine[len(prefix):])
			return strings.TrimSpace(body + "\n" + rest)
		}
	}
	return strings.TrimSpace(rest)
}

func stripTrailingControlLines(s string) string {
	for {
		s = strings.TrimSpace(s)
		lastNL := strings.LastIndex(s, "\n")
		lastLine := s
		if lastNL >= 0 {
			lastLine = s[lastNL+1:]
		}
		tok := normalizeDecisionLine(lastLine)
		if !isPassToken(tok) && tok != "SPEAK" && tok != "REPLY" {
			break
		}
		if lastNL < 0 {
			return ""
		}
		s = strings.TrimSpace(s[:lastNL])
	}
	return s
}

// ChannelDecision is how an LLM channel turn was classified.
type ChannelDecision string

const (
	ChannelDecisionEmpty   ChannelDecision = "empty"
	ChannelDecisionPass    ChannelDecision = "pass"
	ChannelDecisionSpeak   ChannelDecision = "speak"
	ChannelDecisionMissing ChannelDecision = "missing_token"
)

// ClassifyChannelLLMReply interprets raw LLM output for channel.post.
// A real post requires an explicit SPEAK token on the first line. Missing the
// decision token is treated as PASS so bare prose cannot leak into the channel.
func ClassifyChannelLLMReply(raw string) (content string, decision ChannelDecision) {
	s := StripThinkTags(strings.TrimSpace(raw))
	if s == "" {
		return "", ChannelDecisionEmpty
	}
	firstLine, rest, _ := strings.Cut(s, "\n")
	tok := normalizeDecisionLine(firstLine)
	if isPassToken(tok) {
		return "", ChannelDecisionPass
	}
	if isSpeakToken(tok) {
		body := inlineAfterSpeak(strings.TrimSpace(firstLine), rest)
		body = stripTrailingControlLines(body)
		if body == "" {
			return "", ChannelDecisionPass
		}
		return body, ChannelDecisionSpeak
	}
	return "", ChannelDecisionMissing
}

// NormalizeChannelLLMReply interprets raw LLM output for channel.post.
// Returns skip=true when the agent should not post. Otherwise returns cleaned text
// with the SPEAK control line removed.
func NormalizeChannelLLMReply(raw string) (content string, skip bool) {
	content, decision := ClassifyChannelLLMReply(raw)
	return content, decision != ChannelDecisionSpeak
}
