package main

// truncateStr truncates a string to the given max length, appending ".." if truncated.
func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-2] + ".."
}

// boolYesNo formats a boolean as "yes" or "no".
func boolYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
