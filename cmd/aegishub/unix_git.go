//go:build !testunixgit

package main

// unixGitAllowed is false in the production Hub binary. Unix git-connect is
// always ERR_UNKNOWN_PEER (no Serve). Never getenv.
func unixGitAllowed() bool { return false }
