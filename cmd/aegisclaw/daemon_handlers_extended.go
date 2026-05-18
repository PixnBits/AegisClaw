package main

// Additional proxies extracted in Phase 3.3

func makeSessionsListProxy(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if env.AegisHubClient == nil {
			return &api.Response{Error: "AegisHubClient not available"}
		}
		// Forward via generic chat/message path for now
		resp, err := env.AegisHubClient.ForwardChatMessage(ctx, data)
		if err != nil {
			return &api.Response{Error: err.Error()}
		}
		return resp
	}
}

// More handlers can be added following the same pattern.
