package testutil

import (
	"context"
	"encoding/json"
	"sync"

	"AegisClaw/internal/dashboard"
)

// FakeBridge records bridge calls for contract tests.
type FakeBridge struct {
	mu    sync.Mutex
	Calls []RecordedCall
}

type RecordedCall struct {
	Action  string
	Payload json.RawMessage
}

func (f *FakeBridge) Call(ctx context.Context, action string, payload json.RawMessage) (*dashboard.APIResponse, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, RecordedCall{Action: action, Payload: payload})
	f.mu.Unlock()
	return &dashboard.APIResponse{Success: true, Data: json.RawMessage(`{}`)}, nil
}

func (f *FakeBridge) Actions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.Calls))
	for i, c := range f.Calls {
		out[i] = c.Action
	}
	return out
}