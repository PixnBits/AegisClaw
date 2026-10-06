package sanitize

import (
	"encoding/json"
	"regexp"
	"sort"
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
	// Scoped keys (sk-proj-…, sk-ant-api03-…, sk-svcacct-…, sk-admin-…,
	// sk-None-…) have '_' and '-' in the body, so credentialPattern never
	// matched them. The prefix is case-insensitive. See redactCredentials
	// for the guard against plain slugs.
	scopedKeyPattern = regexp.MustCompile(`(?i:sk-(?:proj|ant|svcacct|admin|none)-)([A-Za-z0-9_-]{20,})`)
	internalPathPattern = regexp.MustCompile(`/(etc|var|opt|proc|sys|home|root)/[^\s]*`)
	privateIPPattern = regexp.MustCompile(`\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3})\b`)
	hostnamePattern  = regexp.MustCompile(`\b[a-zA-Z0-9-]+\.(internal|local|svc|cluster)\b`)
)

const redacted = "[REDACTED]"

// redactCredentials replaces every credentialPattern match and every
// scoped key with "[REDACTED]". Both patterns are matched on the same input
// and the union of their spans is redacted, so a scoped key whose body
// happens to contain a credentialPattern run is still redacted whole, and
// every span main's pattern redacts is still redacted. Overlapping spans
// merge into one marker; with no scoped keys present the output is exactly
// credentialPattern.ReplaceAllString(s, redacted).
//
// A scoped match counts only when its body has both an upper-case and a
// lower-case letter. Issued keys are random base64url, so they always do.
// Single-case slugs such as "task-proj-refactorauthenticationmodule-v2"
// (channel ids are lower-case only) don't, and are left alone. Trade-off: a
// scoped key that was upper- or lower-cased in transit isn't caught by this
// pass; main never caught scoped keys at all.
func redactCredentials(s string) string {
	spans := credentialPattern.FindAllStringIndex(s, -1)
	for _, m := range scopedKeyPattern.FindAllStringSubmatchIndex(s, -1) {
		body := s[m[2]:m[3]]
		if strings.ContainsAny(body, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") && strings.ContainsAny(body, "abcdefghijklmnopqrstuvwxyz") {
			spans = append(spans, []int{m[0], m[1]})
		}
	}
	if len(spans) == 0 {
		return s
	}
	sort.Slice(spans, func(a, b int) bool { return spans[a][0] < spans[b][0] })
	var b strings.Builder
	b.Grow(len(s))
	last := 0
	start, end := spans[0][0], spans[0][1]
	for _, sp := range spans[1:] {
		if sp[0] < end {
			if sp[1] > end {
				end = sp[1]
			}
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(redacted)
		last = end
		start, end = sp[0], sp[1]
	}
	b.WriteString(s[last:start])
	b.WriteString(redacted)
	b.WriteString(s[end:])
	return b.String()
}

// Text applies context-aware redaction to a plain string for browser display.
func Text(ctx Context, raw string) string {
	if raw == "" {
		return ""
	}
	s := raw
	s = apiKeyPattern.ReplaceAllString(s, "$1: "+redacted)
	s = redactCredentials(s)
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