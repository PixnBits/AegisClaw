package memory

import (
	"regexp"
)

// Scrubber redacts common PII patterns from text before it is stored in the
// memory vault.  It is intentionally conservative: it only replaces patterns
// that are highly likely to be sensitive (email addresses, US phone numbers,
// US SSNs, IPv4 addresses, JWT tokens, and AWS access keys).
//
// Redaction is opt-in.  Enable it by setting memory.pii_redaction: true in
// config.yaml and calling Store.SetScrubber(NewScrubber()) at startup.
type Scrubber struct {
	rules []scrubRule
}

type scrubRule struct {
	label       string
	re          *regexp.Regexp
	replacement string
}

// NewScrubber returns a Scrubber pre-loaded with the default PII rules.
func NewScrubber() *Scrubber {
	rules := []scrubRule{
		// Email addresses — RFC 5322 approximation.
		{
			label:       "email",
			re:          regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
			replacement: "[REDACTED:EMAIL]",
		},
		// US phone numbers in common formats.
		{
			label:       "us-phone",
			re:          regexp.MustCompile(`\b(\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]\d{3}[-.\s]\d{4}\b`),
			replacement: "[REDACTED:PHONE]",
		},
		// US Social Security Numbers (SSN) — NNN-NN-NNNN.
		{
			label:       "ssn",
			re:          regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
			replacement: "[REDACTED:SSN]",
		},
		// IPv4 addresses (but not version numbers like "1.2.3.4" in semver context).
		{
			label:       "ipv4",
			re:          regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`),
			replacement: "[REDACTED:IP]",
		},
		// JWT tokens (header.payload.signature format, all Base64url).
		{
			label:       "jwt",
			re:          regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`),
			replacement: "[REDACTED:JWT]",
		},
		// AWS Access Key IDs — exactly 20 uppercase alphanumerics starting with AKIA/AKID/etc.
		{
			label:       "aws-key",
			re:          regexp.MustCompile(`\b(AKIA|AKID|ABIA|ACCA|ASIA)[A-Z0-9]{16}\b`),
			replacement: "[REDACTED:AWS-KEY]",
		},
		// Generic API keys / tokens — long (≥32 char) hex or Base64 strings that look like secrets.
		// Matches patterns like: secret=<long_string>, token=<long_string>, key=<long_string>.
		{
			label:       "generic-secret",
			re:          regexp.MustCompile(`(?i)(secret|token|api[-_]?key|password|passwd|auth)[\s:='"]+([A-Za-z0-9+/=\-_]{32,})`),
			replacement: `[REDACTED:SECRET]`,
		},
	}
	return &Scrubber{rules: rules}
}

// Scrub returns a copy of text with all matching PII patterns replaced.
func (s *Scrubber) Scrub(text string) string {
	for _, r := range s.rules {
		if r.label == "generic-secret" {
			// For labeled secrets, replace only the value (group 2), preserving the key name.
			text = r.re.ReplaceAllStringFunc(text, func(match string) string {
				// Re-run to find where the value starts.
				m := r.re.FindStringSubmatchIndex(match)
				if len(m) >= 6 {
					return match[:m[4]] + "[REDACTED:SECRET]"
				}
				return "[REDACTED:SECRET]"
			})
		} else {
			text = r.re.ReplaceAllString(text, r.replacement)
		}
	}
	return text
}

// ScrubEntry applies the scrubber to both key and value of a MemoryEntry,
// returning a modified copy.  Tags are left unchanged.
func (s *Scrubber) ScrubEntry(e *MemoryEntry) *MemoryEntry {
	if e == nil {
		return nil
	}
	return &MemoryEntry{
		MemoryID:        e.MemoryID,
		Key:             s.Scrub(e.Key),
		Value:           s.Scrub(e.Value),
		Tags:            e.Tags,
		TTLTier:         e.TTLTier,
		TaskID:          e.TaskID,
		SecurityLevel:   e.SecurityLevel,
		CreatedAt:       e.CreatedAt,
		LastCompactedAt: e.LastCompactedAt,
		Version:         e.Version,
		Deleted:         e.Deleted,
	}
}

// ContainsPII returns true if the text matches any PII rule.  Useful for
// logging/debugging without actually storing the content.
func (s *Scrubber) ContainsPII(text string) bool {
	for _, r := range s.rules {
		if r.re.MatchString(text) {
			return true
		}
	}
	return false
}

// Labels returns the names of all active PII rules.
func (s *Scrubber) Labels() []string {
	names := make([]string, len(s.rules))
	for i, r := range s.rules {
		names[i] = r.label
	}
	return names
}
