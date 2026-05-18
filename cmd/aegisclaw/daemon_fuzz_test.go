package main

import (
	"testing"
)

// FuzzWithAuthorizedCaller is an initial fuzz target for authorization logic.
// This is the starting point for fuzz testing as outlined in the test backlog.
func FuzzWithAuthorizedCaller(f *testing.F) {
	// Seed with some example inputs
	f.Add([]byte(`{"user_id": "test"}`))
	f.Add([]byte(``))
	f.Add([]byte(`{}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		// TODO: Call authorization logic with fuzzed input
		// For now this is a skeleton.
		_ = data
	})
}

// Future fuzz targets (from backlog):
// - Unix socket message parsing
// - VM spec / config parsing
// - Key distribution messages
