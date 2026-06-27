package permissions

import "strings"

// SubjectMatches reports whether subjectID matches a grant/visibility pattern.
// Patterns support trailing wildcard (e.g. "project-manager*", "coder*").
func SubjectMatches(subjectID, pattern string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(subjectID, prefix)
	}
	return subjectID == pattern
}

// PersonaPattern extracts the persona prefix from a component ID (e.g. "project-manager-abc" -> "project-manager-*").
func PersonaPattern(subjectID string) string {
	if idx := strings.LastIndex(subjectID, "-"); idx > 0 {
		return subjectID[:idx+1] + "*"
	}
	return subjectID
}
