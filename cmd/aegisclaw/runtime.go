package main

// stubAegisHubClient implementation aligned with proxy functions
func (s *stubAegisHubClient) ForwardChatMessage(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return &api.Response{Success: true, Data: []byte(`{"status":"forwarded to AegisHub (stub)"}`)}, nil
}

func (s *stubAegisHubClient) ForwardChatTool(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return &api.Response{Success: true, Data: []byte(`{"status":"tool forwarded to AegisHub (stub)"}`)}, nil
}
