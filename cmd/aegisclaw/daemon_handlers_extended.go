package main

func makeChatSummarizeProxy(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if env.AegisHubClient == nil {
			return &api.Response{Error: "AegisHubClient not available"}
		}
		// For now we reuse ForwardChatMessage as a generic forwarder.
		// A dedicated ForwardChatSummarize can be added later.
		resp, err := env.AegisHubClient.ForwardChatMessage(ctx, data)
		if err != nil {
			return &api.Response{Error: err.Error()}
		}
		return resp
	}
}

func makeChatSlashProxy(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if env.AegisHubClient == nil {
			return &api.Response{Error: "AegisHubClient not available"}
		}
		resp, err := env.AegisHubClient.ForwardChatMessage(ctx, data)
		if err != nil {
			return &api.Response{Error: err.Error()}
		}
		return resp
	}
}
