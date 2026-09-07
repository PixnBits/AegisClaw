//go:build testunixgit

package main

// unixGitAllowed is true only when Hub is built with -tags testunixgit
// (T3/T4 test hubBin). Production go build does not set this.
func unixGitAllowed() bool { return true }
