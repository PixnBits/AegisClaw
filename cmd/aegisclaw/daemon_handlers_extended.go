package main

// Phase 3.3: Actual proxy implementations for chat handlers.
// These now forward to AegisHubClient instead of executing logic locally.

func makeChatMessageProxy(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if env.AegisHubClient == nil {
			return &api.Response{Error: "AegisHubClient not initialized"}
		}
		resp, err := env.AegisHubClient.ForwardChatMessage(ctx, data)
		if err != nil {
			return &api.Response{Error: "AegisHub forward failed: " + err.Error()}
		}
		return resp
	}
}

func makeChatToolProxy(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if env.AegisHubClient == nil {
			return &api.Response{Error: "AegisHubClient not initialized"}
		}
		resp, err := env.AegisHubClient.ForwardChatTool(ctx, data)
		if err != nil {
			return &api.Response{Error: "AegisHub tool forward failed: " + err.Error()}
		}
		return resp
	}
}

// Update in registerExtendedDaemonAPI:
// Replace old makeChat*Handler calls with the new proxies:
// apiSrv.Handle("chat.message", makeChatMessageProxy(env))
// apiSrv.Handle("chat.tool", withAuthorizedCaller(env, "chat.tool", makeChatToolProxy(env)))
