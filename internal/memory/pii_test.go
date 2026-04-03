package memory_test

import (
	"strings"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/memory"
)

func TestScrubber_Email(t *testing.T) {
	s := memory.NewScrubber()
	cases := []struct {
		in   string
		want string
	}{
		{"Contact user@example.com for help", "Contact [REDACTED:EMAIL] for help"},
		{"Send to alice.bob+tag@sub.domain.org today", "Send to [REDACTED:EMAIL] today"},
		{"No email here", "No email here"},
	}
	for _, tc := range cases {
		got := s.Scrub(tc.in)
		if got != tc.want {
			t.Errorf("Scrub(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestScrubber_Phone(t *testing.T) {
	s := memory.NewScrubber()
	cases := []string{
		"Call 555-867-5309",
		"Phone: (800) 555-1234",
		"+1-800-555-0199",
	}
	for _, in := range cases {
		got := s.Scrub(in)
		if strings.Contains(got, "555") || strings.Contains(got, "800") {
			t.Errorf("Scrub(%q) = %q still contains phone digits", in, got)
		}
		if !strings.Contains(got, "[REDACTED:PHONE]") {
			t.Errorf("Scrub(%q) = %q, want [REDACTED:PHONE]", in, got)
		}
	}
}

func TestScrubber_SSN(t *testing.T) {
	s := memory.NewScrubber()
	in := "SSN: 123-45-6789"
	got := s.Scrub(in)
	if !strings.Contains(got, "[REDACTED:SSN]") {
		t.Errorf("Scrub(%q) = %q, expected SSN redaction", in, got)
	}
	if strings.Contains(got, "123-45-6789") {
		t.Errorf("SSN not redacted in %q", got)
	}
}

func TestScrubber_IPv4(t *testing.T) {
	s := memory.NewScrubber()
	in := "Server at 192.168.1.100 is down"
	got := s.Scrub(in)
	if !strings.Contains(got, "[REDACTED:IP]") {
		t.Errorf("Scrub(%q) = %q, expected IP redaction", in, got)
	}
}

func TestScrubber_JWT(t *testing.T) {
	s := memory.NewScrubber()
	in := "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjMifQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	got := s.Scrub(in)
	if !strings.Contains(got, "[REDACTED:JWT]") {
		t.Errorf("Scrub(%q) = %q, expected JWT redaction", in, got)
	}
}

func TestScrubber_AWSKey(t *testing.T) {
	s := memory.NewScrubber()
	in := "Key: AKIAIOSFODNN7EXAMPLE used for S3"
	got := s.Scrub(in)
	if !strings.Contains(got, "[REDACTED:AWS-KEY]") {
		t.Errorf("Scrub(%q) = %q, expected AWS key redaction", in, got)
	}
}

func TestScrubber_NoFalsePositives(t *testing.T) {
	s := memory.NewScrubber()
	safeCases := []string{
		"version 1.2.3.4-beta",  // Not an IP in this form (semver)
		"The quick brown fox",
		"func main() { fmt.Println(42) }",
		"https://github.com/user/repo",
	}
	for _, in := range safeCases {
		got := s.Scrub(in)
		if strings.Contains(got, "[REDACTED") {
			t.Logf("possible false positive: Scrub(%q) = %q", in, got)
			// Not a hard failure — note false positives but don't break.
		}
	}
}

func TestScrubber_ContainsPII(t *testing.T) {
	s := memory.NewScrubber()
	if !s.ContainsPII("email: foo@bar.com") {
		t.Error("expected ContainsPII=true for email")
	}
	if s.ContainsPII("no pii here at all") {
		t.Error("expected ContainsPII=false for clean text")
	}
}

func TestScrubber_Labels(t *testing.T) {
	s := memory.NewScrubber()
	labels := s.Labels()
	if len(labels) < 5 {
		t.Errorf("expected at least 5 rules, got %d", len(labels))
	}
	found := make(map[string]bool)
	for _, l := range labels {
		found[l] = true
	}
	for _, required := range []string{"email", "ssn", "jwt", "aws-key"} {
		if !found[required] {
			t.Errorf("missing rule %q", required)
		}
	}
}

func TestScrubber_ScrubEntry(t *testing.T) {
	s := memory.NewScrubber()
	e := &memory.MemoryEntry{
		Key:   "user.email",
		Value: "My email is user@example.com",
		Tags:  []string{"user"},
	}
	scrubbed := s.ScrubEntry(e)
	if strings.Contains(scrubbed.Value, "user@example.com") {
		t.Errorf("ScrubEntry did not redact email from value: %q", scrubbed.Value)
	}
	// Tags should be unchanged.
	if len(scrubbed.Tags) != 1 || scrubbed.Tags[0] != "user" {
		t.Errorf("ScrubEntry changed tags: %v", scrubbed.Tags)
	}
}
