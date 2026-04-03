package main

import (
	"fmt"
	"time"

	"github.com/PixnBits/AegisClaw/internal/worker"
)

// formatWorkerRecord returns a multi-line human-readable summary of a worker record.
func formatWorkerRecord(w *worker.WorkerRecord) string {
	dur := ""
	if w.FinishedAt != nil {
		dur = fmt.Sprintf("  Duration: %s\n", w.FinishedAt.Sub(w.SpawnedAt).Round(time.Second))
	}
	result := ""
	if w.Result != "" {
		result = fmt.Sprintf("  Result:   %s\n", truncate(w.Result, 200))
	}
	errStr := ""
	if w.Error != "" {
		errStr = fmt.Sprintf("  Error:    %s\n", truncate(w.Error, 120))
	}
	tools := ""
	if len(w.ToolsGranted) > 0 {
		tools = fmt.Sprintf("  Tools:    %v\n", w.ToolsGranted)
	}
	return fmt.Sprintf(
		"Worker %s\n"+
			"  Role:     %s\n"+
			"  Status:   %s\n"+
			"  Steps:    %d\n"+
			"  Task:     %s\n"+
			"  TaskID:   %s\n"+
			"  Spawned:  %s\n"+
			"%s%s%s%s",
		w.WorkerID,
		w.Role, w.Status, w.StepCount,
		truncate(w.TaskDescription, 80),
		w.TaskID,
		w.SpawnedAt.Format(time.RFC3339),
		dur, tools, result, errStr,
	)
}
