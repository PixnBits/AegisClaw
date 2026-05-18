package main

// AegisHubClient is the Phase 3.3 seam for forwarding control-plane
// requests (chat, tools, etc.) to AegisHub.
type AegisHubClient interface {
	ForwardChatMessage(ctx context.Context, data json.RawMessage) (*api.Response, error)
	ForwardChatTool(ctx context.Context, data json.RawMessage) (*api.Response, error)
	// Add more methods as needed
}

// stubAegisHubClient is a transitional implementation.
type stubAegisHubClient struct{}

func (s *stubAegisHubClient) ForwardChatMessage(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return &api.Response{Error: "chat forwarded to AegisHub (stub)"}, nil
}

func (s *stubAegisHubClient) ForwardChatTool(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return &api.Response{Error: "chat tool forwarded to AegisHub (stub)"}, nil
}
