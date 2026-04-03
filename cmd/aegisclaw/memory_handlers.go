package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/memory"
)

type memoryListRequest struct {
	Tier      string `json:"tier,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	CountOnly bool   `json:"count_only,omitempty"`
}

type memorySearchRequest struct {
	Query  string `json:"query"`
	K      int    `json:"k,omitempty"`
	TaskID string `json:"task_id,omitempty"`
}

func makeMemoryListHandler(env *runtimeEnv) api.Handler {
	return func(_ctx context.Context, data json.RawMessage) *api.Response {
		if env.MemoryStore == nil {
			return &api.Response{Error: "memory store unavailable"}
		}

		var req memoryListRequest
		if len(data) > 0 {
			if err := json.Unmarshal(data, &req); err != nil {
				return &api.Response{Error: "invalid request: " + err.Error()}
			}
		}

		if req.CountOnly {
			out, _ := json.Marshal(map[string]interface{}{"total": env.MemoryStore.Count()})
			return &api.Response{Success: true, Data: out}
		}

		limit := req.Limit
		if limit <= 0 {
			limit = 50
		}

		results, err := env.MemoryStore.Retrieve("", limit, "")
		if err != nil {
			return &api.Response{Error: "memory list failed: " + err.Error()}
		}

		if req.Tier != "" {
			tier := memory.TTLTier(req.Tier)
			filtered := make([]*memory.MemoryEntry, 0, len(results))
			for _, e := range results {
				if e.TTLTier == tier {
					filtered = append(filtered, e)
				}
			}
			results = filtered
		}

		out, _ := json.Marshal(results)
		return &api.Response{Success: true, Data: out}
	}
}

func makeMemorySearchHandler(env *runtimeEnv) api.Handler {
	return func(_ctx context.Context, data json.RawMessage) *api.Response {
		if env.MemoryStore == nil {
			return &api.Response{Error: "memory store unavailable"}
		}

		var req memorySearchRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Query == "" {
			return &api.Response{Error: "query is required"}
		}

		k := req.K
		if k <= 0 {
			k = 20
		}
		results, err := env.MemoryStore.Retrieve(req.Query, k, req.TaskID)
		if err != nil {
			return &api.Response{Error: fmt.Sprintf("memory search failed: %v", err)}
		}

		out, _ := json.Marshal(results)
		return &api.Response{Success: true, Data: out}
	}
}
