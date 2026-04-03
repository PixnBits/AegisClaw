package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"go.uber.org/zap"
)

const (
	// memoryCompactionInterval is how often the background daemon runs.
	memoryCompactionInterval = 24 * time.Hour
)

// startMemoryCompactionDaemon launches a background goroutine that runs the
// Memory Store compaction process on a daily schedule.  If
// Config.Memory.CompactOnStartup is set it also runs once immediately.
//
// The goroutine exits when ctx is cancelled (daemon shutdown).
func startMemoryCompactionDaemon(ctx context.Context, env *runtimeEnv) {
	if env.MemoryStore == nil {
		return
	}

	runCompact := func() {
		result, err := env.MemoryStore.CompactAll()
		if err != nil {
			env.Logger.Error("memory compaction failed", zap.Error(err))
			return
		}
		if result.Compacted > 0 || result.Examined > 0 {
			// Audit-log the compaction run.
			auditPayload, _ := json.Marshal(map[string]interface{}{
				"source":    "daemon-cron",
				"examined":  result.Examined,
				"compacted": result.Compacted,
			})
			act := kernel.NewAction(kernel.ActionMemoryCompact, "daemon", auditPayload)
			env.Kernel.SignAndLog(act) //nolint:errcheck
			env.Logger.Info("memory compaction complete",
				zap.Int("examined", result.Examined),
				zap.Int("compacted", result.Compacted),
				zap.Duration("elapsed", result.ElapsedTime),
			)
		}
	}

	go func() {
		if env.Config.Memory.CompactOnStartup {
			runCompact()
		}
		ticker := time.NewTicker(memoryCompactionInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runCompact()
			}
		}
	}()
}

// buildMemoryAutoSummaryMsg constructs the memory summary message injected
// into the system prompt before a new agent turn.  It retrieves the top-k
// entries matching the user's query and formats them as bullet points.
// Returns an empty string when no relevant memories are found.
func buildMemoryAutoSummaryMsg(store *memory.Store, query string, k int) string {
	if store == nil || k <= 0 {
		return ""
	}
	results, err := store.Retrieve(query, k, "")
	if err != nil || len(results) == 0 {
		return ""
	}
	var b []byte
	for _, e := range results {
		line := "- [" + string(e.TTLTier) + "] " + e.Key + ": " + truncate(e.Value, 300) + "\n"
		b = append(b, []byte(line)...)
	}
	return string(b)
}
