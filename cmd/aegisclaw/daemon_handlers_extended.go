package main

func makeApprovalsListProxy(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if env.AegisHubClient == nil {
			return &api.Response{Error: "AegisHubClient not available"}
		}
		resp, err := env.AegisHubClient.ForwardApprovalsList(ctx, data)
		if err != nil {
			return &api.Response{Error: err.Error()}
		}
		return resp
	}
}

func makeApprovalsDecideProxy(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if env.AegisHubClient == nil {
			return &api.Response{Error: "AegisHubClient not available"}
		}
		resp, err := env.AegisHubClient.ForwardApprovalsDecide(ctx, data)
		if err != nil {
			return &api.Response{Error: err.Error()}
		}
		return resp
	}
}
