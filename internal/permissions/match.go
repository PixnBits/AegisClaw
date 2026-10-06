package permissions

import "strings"

// SubjectMatches reports whether subjectID matches a grant/visibility pattern.
// "*" matches every id. A trailing "*" whose prefix already ends in '-' or '.'
// stays a raw prefix match ("court-persona-*", "memory.*"). Any other
// "<prefix>*" matches only id == prefix or an id starting with prefix+"-",
// so "project-manager*" matches "project-manager" and "project-manager-1"
// but not "project-managerX".
func SubjectMatches(subjectID, pattern string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		if prefix != "" && !strings.HasSuffix(prefix, "-") && !strings.HasSuffix(prefix, ".") {
			return subjectID == prefix || strings.HasPrefix(subjectID, prefix+"-")
		}
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
