package main

import (
	"testing"
)

// FuzzWithAuthorizedCaller fuzzes the authorization wrapper input.
// This is the initial high-priority fuzz target from the test backlog.
func FuzzWithAuthorizedCaller(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"session_id":"abc"}`))
	f.Add([]byte(`invalid json`))
	f.Add([]byte(``))

	f.Fuzz(func(t *testing.T, input []byte) {
		// TODO: Wire real authorization logic here when ready.
		// For now we just ensure it doesn't panic on bad input.
		_ = input
	})
}
