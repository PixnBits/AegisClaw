package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/lookup"
	"go.uber.org/zap"
)

type lookupSearchRequest struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
}

// makeLookupSearchHandler returns an API handler that performs a semantic
// vector search over the indexed tool collection and returns raw Gemma 4
// control-token blocks.
func makeLookupSearchHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if env.LookupStore == nil {
			return &api.Response{Error: "lookup store unavailable"}
		}

		var req lookupSearchRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Query == "" {
			return &api.Response{Error: "query is required"}
		}
		if req.MaxResults <= 0 {
			req.MaxResults = 6
		}

		results, err := env.LookupStore.LookupTools(ctx, req.Query, req.MaxResults)
		if err != nil {
			return &api.Response{Error: fmt.Sprintf("lookup search failed: %v", err)}
		}

		out, _ := json.Marshal(results)
		return &api.Response{Success: true, Data: out}
	}
}

// makeLookupListHandler returns an API handler that lists all indexed tools
// (name + description only, no embeddings).
func makeLookupListHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		if env.LookupStore == nil {
			return &api.Response{Error: "lookup store unavailable"}
		}

		out, _ := json.Marshal(map[string]int{"indexed": env.LookupStore.Count()})
		return &api.Response{Success: true, Data: out}
	}
}

// seedLookupStore indexes all built-in daemon tools into the lookup store so
// they are immediately discoverable via lookup_tools.  This runs once at daemon
// startup in a background goroutine to avoid blocking the start sequence.
// If the lookup store is nil (e.g. disabled or errored during init) this is a
// no-op.
func seedLookupStore(ctx context.Context, env *runtimeEnv, reg *ToolRegistry) {
	if env.LookupStore == nil {
		return
	}

	go func() {
		seeded := 0
		skipped := 0
		for name, meta := range reg.meta {
			if meta.Description == "" {
				skipped++
				continue
			}
			// Derive skill_name from the qualified tool name (e.g. "memory.store" → "memory").
			skillName := ""
			if idx := strings.Index(name, "."); idx > 0 {
				skillName = name[:idx]
			}

			if err := env.LookupStore.IndexTool(ctx, lookup.ToolEntry{
				Name:        name,
				Description: meta.Description,
				SkillName:   skillName,
			}); err != nil {
				env.Logger.Warn("lookup: failed to seed built-in tool",
					zap.String("tool", name),
					zap.Error(err),
				)
				continue
			}
			seeded++
		}
		env.Logger.Info("lookup: built-in tools seeded",
			zap.Int("seeded", seeded),
			zap.Int("skipped", skipped),
			zap.Int("total_indexed", env.LookupStore.Count()),
		)
	}()
}
