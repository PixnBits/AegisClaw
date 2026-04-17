// Package exec defines the TaskExecutor interface and shared types for
// executing one turn of the agent's ReAct loop.
//
// The interface abstracts the communication channel between the daemon and the
// agent so that:
//   - Production builds always use FirecrackerTaskExecutor, which routes calls
//     through the Firecracker microVM + jailer (the only supported path).
//   - Tests compiled with the "inprocesstest" build tag may use
//     InProcessTaskExecutor to run the agent logic directly in the test
//     process, eliminating the need for a live KVM environment.
//
// Security note: InProcessTaskExecutor is compiled ONLY when the
// "inprocesstest" build tag is present. It MUST NOT appear in any production
// binary. See CONTRIBUTING.md for details.
package exec

import "context"

// AgentMessage is a single message in an agent conversation.
type AgentMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// AgentTurnRequest is sent to the executor for one iteration of the ReAct loop.
type AgentTurnRequest struct {
	// Messages is the full conversation history including system prompt,
	// previous turns, and any accumulated tool results.
	Messages []AgentMessage `json:"messages"`

	// Model is the Ollama model name to use (e.g. "llama3.2:3b").
	// If empty the executor uses its configured default.
	Model string `json:"model,omitempty"`

	// StreamID, if set, is forwarded to the agent for SSE progress streaming.
	StreamID string `json:"stream_id,omitempty"`

	// StructuredOutput requests JSON-mode enforcement from the agent.
	StructuredOutput bool `json:"structured_output,omitempty"`

	// TraceID is an optional correlation ID that should appear in logs,
	// audit entries, and portal events to make end-to-end debugging easier.
	// If empty, the executor generates no correlation context of its own.
	TraceID string `json:"trace_id,omitempty"`
}

// AgentTurnResponse is the executor's response for one ReAct iteration.
type AgentTurnResponse struct {
	// Status is either "final" (done) or "tool_call" (needs a tool result).
	Status string `json:"status"`

	// Content is the assistant's final answer (Status=="final").
	Content string `json:"content,omitempty"`

	// Thinking contains the model's internal reasoning text, if any.
	Thinking string `json:"thinking,omitempty"`

	// Tool is the qualified tool name to call (Status=="tool_call").
	Tool string `json:"tool,omitempty"`

	// Args is the JSON-encoded arguments for the tool call.
	Args string `json:"args,omitempty"`
}

// TaskExecutor handles one agent turn in the ReAct loop.
//
// Each call to ExecuteTurn corresponds to one LLM inference step.  The caller
// is responsible for driving the full loop: appending tool results and calling
// ExecuteTurn again until Status=="final" or the iteration cap is reached.
//
// All implementations must be safe for concurrent use.
type TaskExecutor interface {
	// ExecuteTurn sends the current conversation to the agent and returns
	// its response.  It must be idempotent with respect to the conversation
	// state — the caller owns the message list.
	ExecuteTurn(ctx context.Context, req AgentTurnRequest) (AgentTurnResponse, error)
}
