package loop

import "time"

// Config controls hard safety limits for a single agentic loop run.
type Config struct {
	MaxIterations int
	PerCallTimeout time.Duration
	TotalTimeout time.Duration
}

// ToolCall is a normalized tool invocation emitted by the model.
type ToolCall struct {
	Name string
	ArgsJSON string
}

// TraceEvent captures loop progress for streaming to host telemetry.
type TraceEvent struct {
	Phase string
	Summary string
	Details string
	Timestamp time.Time
}
