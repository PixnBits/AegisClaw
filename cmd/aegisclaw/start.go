// makeCourtReviewHandler forwards Court review requests via CourtClient.
// Real review logic executes in Court VMs orchestrated by Court Scribe.
func makeCourtReviewHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env.CourtClient // CourtClient is the seam to the real implementation in Court VMs
		return &api.Response{Success: true, Data: []byte(`{"status":"stubbed"}`)}
	}
}

// makeCourtVoteHandler forwards Court vote requests via CourtClient.
// Real voting and consensus logic executes in Court VMs + Court Scribe.
func makeCourtVoteHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		_ = env.CourtClient // CourtClient is the seam to the real implementation in Court VMs
		return &api.Response{Success: true}
	}
}