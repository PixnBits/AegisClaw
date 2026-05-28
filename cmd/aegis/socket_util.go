package main

import "strings"

// isAbstractSocket reports whether the given address refers to a Linux
// abstract Unix socket (names that start with a null byte when passed to
// net.Listen / net.Dial).
func isAbstractSocket(addr string) bool {
	return strings.HasPrefix(addr, "\x00") || strings.HasPrefix(addr, "@")
}
