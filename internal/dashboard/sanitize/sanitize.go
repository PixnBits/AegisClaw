package sanitize

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Context selects sanitization rules per docs/specs/web-portal/security-boundaries.md.
type Context int

const (
	ContextChat Context = iota
	ContextTrace
	ContextProposal
)

var (
	apiKeyPattern    = regexp.MustCompile(`(?i)(api[_-]?key|secret|password|token|bearer)\s*[:=]\s*\S+`)
	credentialPattern = regexp.MustCompile(`(?i)(AKIA[0-9A-Z]{16}|sk-[a-zA-Z0-9]{20,})`)
	// Scoped keys (sk-proj-…, sk-ant-api03-…, sk-svcacct-…, sk-admin-…)
	// have '_' and '-' in the body, so credentialPattern never matched
	// them. See redactScopedKey for the guard against plain slugs. The
	// "[REDACTED]" prefix covers a key glued after an sk- key: the first
	// pass eats the "sk" and leaves "[REDACTED]-proj-<body>".
	scopedKeyPattern = regexp.MustCompile(`(?i:(?:sk|\[REDACTED\])-(?:proj|ant|svcacct|admin)-)([A-Za-z0-9_-]{20,})`)
	internalPathPattern = regexp.MustCompile(`/(etc|var|opt|proc|sys|home|root)/[^\s]*`)
	privateIPPattern = regexp.MustCompile(`\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3})\b`)
	hostnamePattern  = regexp.MustCompile(`\b[a-zA-Z0-9-]+\.(internal|local|svc|cluster)\b`)
)

const redacted = "[REDACTED]"

// redactScopedKey redacts a scopedKeyPattern match when its body has both
// an upper-case and a lower-case letter. Issued keys are random base64url,
// so they always do. Single-case slugs such as
// "task-proj-refactorauthenticationmodule-v2" (channel ids are lower-case
// only) don't, and are left alone. Trade-off: a scoped key that was
// upper- or lower-cased in transit isn't caught. main never caught scoped
// keys at all, so this is never weaker than before.
func redactScopedKey(m string) string {
	sub := scopedKeyPattern.FindStringSubmatch(m)
	if len(sub) != 2 {
		return m
	}
	body := sub[1]
	if strings.ContainsAny(body, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") && strings.ContainsAny(body, "abcdefghijklmnopqrstuvwxyz") {
		return redacted
	}
	return m
}

// Text applies context-aware redaction to a plain string for browser display.
func Text(ctx Context, raw string) string {
	if raw == "" {
		return ""
	}
	s := raw
	s = apiKeyPattern.ReplaceAllString(s, "$1: "+redacted)
	s = credentialPattern.ReplaceAllString(s, redacted)
	// After credentialPattern, so everything it redacts stays redacted:
	// this pass only replaces runs of [A-Za-z0-9_-] (optionally led by an
	// existing "[REDACTED]") with "[REDACTED]".
	s = scopedKeyPattern.ReplaceAllStringFunc(s, redactScopedKey)
	s = internalPathPattern.ReplaceAllString(s, redacted)
	s = privateIPPattern.ReplaceAllString(s, redacted)
	s = hostnamePattern.ReplaceAllString(s, redacted)

	switch ctx {
	case ContextChat:
		s = stripHTML(s)
	case ContextTrace:
		if len(s) > 8000 {
			s = s[:8000] + "…"
		}
	case ContextProposal:
		// rationales shown in full unless sensitive patterns matched above
	}
	return s
}

// JSONMap strips internal fields and sanitizes string values in a map destined for the browser.
func JSONMap(ctx Context, m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		if isInternalField(k) {
			continue
		}
		out[k] = sanitizeValue(ctx, v)
	}
	return out
}

// Value sanitizes an arbitrary JSON-serializable value for browser responses.
func Value(ctx Context, v interface{}) interface{} {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	clean, err := JSONBytes(ctx, b)
	if err != nil {
		return v
	}
	var out interface{}
	if err := json.Unmarshal(clean, &out); err != nil {
		return v
	}
	return out
}

// JSONBytes parses, sanitizes, and re-marshals JSON for STOMP/SSE payloads.
func JSONBytes(ctx Context, body []byte) ([]byte, error) {
	var v interface{}
	if err := json.Unmarshal(body, &v); err != nil {
		return body, err
	}
	clean := sanitizeValue(ctx, v)
	return json.Marshal(clean)
}

func sanitizeValue(ctx Context, v interface{}) interface{} {
	switch t := v.(type) {
	case string:
		return Text(ctx, t)
	case map[string]interface{}:
		return JSONMap(ctx, t)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, item := range t {
			out[i] = sanitizeValue(ctx, item)
		}
		return out
	default:
		return v
	}
}

func isInternalField(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	switch k {
	case "agent_instance_id", "vm_id", "vsock_addr", "internal_addr", "hub_addr":
		return true
	}
	return strings.HasPrefix(k, "_internal")
}

func stripHTML(s string) string {
	s = strings.ReplaceAll(s, "<script", "&lt;script")
	s = strings.ReplaceAll(s, "</script", "&lt;/script")
	s = strings.ReplaceAll(s, "<iframe", "&lt;iframe")
	return s
}