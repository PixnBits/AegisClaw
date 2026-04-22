package main

import (
	"context"
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/api"
)

// makeKBWikiListHandler returns an API handler that lists all compiled wiki pages.
func makeKBWikiListHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		if env.KBStore == nil {
			return &api.Response{Success: false, Error: "knowledge base not initialised"}
		}
		pages, err := env.KBStore.ListWikiPages()
		if err != nil {
			return &api.Response{Success: false, Error: err.Error()}
		}
		data, _ := json.Marshal(pages)
		return &api.Response{Success: true, Data: data}
	}
}

// makeKBWikiGetHandler returns an API handler that fetches a single wiki page by slug.
func makeKBWikiGetHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		if env.KBStore == nil {
			return &api.Response{Success: false, Error: "knowledge base not initialised"}
		}
		var req struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(data, &req); err != nil || req.Slug == "" {
			return &api.Response{Success: false, Error: "slug is required"}
		}
		page, content, err := env.KBStore.GetWikiPage(req.Slug)
		if err != nil {
			return &api.Response{Success: false, Error: err.Error()}
		}
		resp, _ := json.Marshal(map[string]interface{}{
			"page":    page,
			"content": content,
		})
		return &api.Response{Success: true, Data: resp}
	}
}

// makeKBStatusHandler returns an API handler that reports KB status metrics.
func makeKBStatusHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		if env.KBStore == nil {
			return &api.Response{Success: false, Error: "knowledge base not initialised"}
		}
		status, err := env.KBStore.Status()
		if err != nil {
			return &api.Response{Success: false, Error: err.Error()}
		}
		data, _ := json.Marshal(status)
		return &api.Response{Success: true, Data: data}
	}
}
