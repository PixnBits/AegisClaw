package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

// startKBCompilerDaemon launches a background goroutine that runs the KB
// Compiler on the schedule configured in Config.KnowledgeBase.CompilerIntervalHours.
// It also runs once immediately at startup so the wiki index is always current.
//
// The goroutine exits when ctx is cancelled (daemon shutdown).
func startKBCompilerDaemon(ctx context.Context, env *runtimeEnv) {
	if env.KBStore == nil {
		return
	}

	intervalHours := env.Config.KnowledgeBase.CompilerIntervalHours
	if intervalHours <= 0 {
		intervalHours = 6
	}
	interval := time.Duration(intervalHours) * time.Hour

	runCompile := func() {
		stats, err := env.KBStore.Compile()
		if err != nil {
			env.Logger.Error("kb compiler: compile failed", zap.Error(err))
			return
		}
		payload, _ := json.Marshal(map[string]interface{}{
			"source":        "daemon-cron",
			"raw_examined":  stats.RawExamined,
			"pages_written": stats.PagesWritten,
			"pages_updated": stats.PagesUpdated,
		})
		act := kernel.NewAction(kernel.ActionKBCompile, "daemon", payload)
		env.Kernel.SignAndLog(act) //nolint:errcheck
		env.Logger.Info("kb compiler: run complete",
			zap.Int("raw_examined", stats.RawExamined),
			zap.Int("pages_written", stats.PagesWritten),
			zap.Int("pages_updated", stats.PagesUpdated),
			zap.Duration("elapsed", stats.ElapsedTime),
		)
	}

	go func() {
		// Run immediately on startup to ensure the wiki index is current.
		runCompile()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runCompile()
			}
		}
	}()
}

// startKBLinterDaemon launches a background goroutine that runs the KB Linter
// on the schedule configured in Config.KnowledgeBase.LinterIntervalHours.
//
// The goroutine exits when ctx is cancelled (daemon shutdown).
func startKBLinterDaemon(ctx context.Context, env *runtimeEnv) {
	if env.KBStore == nil {
		return
	}

	intervalHours := env.Config.KnowledgeBase.LinterIntervalHours
	if intervalHours <= 0 {
		intervalHours = 24
	}
	interval := time.Duration(intervalHours) * time.Hour

	runLint := func() {
		stats, err := env.KBStore.Lint()
		if err != nil {
			env.Logger.Error("kb linter: lint failed", zap.Error(err))
			return
		}
		payload, _ := json.Marshal(map[string]interface{}{
			"source":        "daemon-cron",
			"pages_scanned": stats.PagesScanned,
			"issues_found":  stats.IssuesFound,
		})
		act := kernel.NewAction(kernel.ActionKBLint, "daemon", payload)
		env.Kernel.SignAndLog(act) //nolint:errcheck
		if stats.IssuesFound > 0 {
			env.Logger.Warn("kb linter: issues found",
				zap.Int("pages_scanned", stats.PagesScanned),
				zap.Int("issues_found", stats.IssuesFound),
			)
			for _, issue := range stats.Issues {
				env.Logger.Warn("kb linter issue",
					zap.String("page", issue.Page),
					zap.String("kind", issue.Kind),
					zap.String("message", issue.Message),
				)
			}
		} else {
			env.Logger.Info("kb linter: no issues found",
				zap.Int("pages_scanned", stats.PagesScanned),
			)
		}
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runLint()
			}
		}
	}()
}
