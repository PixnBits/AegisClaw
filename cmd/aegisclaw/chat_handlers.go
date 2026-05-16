package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/audit"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/llm"
	rtexec "github.com/PixnBits/AegisClaw/internal/runtime/exec"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/sessions"
	"github.com/PixnBits/AegisClaw/internal/tui"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const reactMaxIterations = 10

// Embedded JSON Schema for the `proposal.create_draft` tool-call payload.
//
//go:embed schemas/proposal-create-draft.schema.json
var proposalCreateDraftSchema string

// GetProposalCreateDraftSchema returns the embedded JSON Schema for use by
// other parts of the daemon (e.g., documentation, validators, or LLM
// prompt generation). Keeping the schema embedded ensures the binary is
// self-contained and the schema is versioned alongside the code.
func GetProposalCreateDraftSchema() string {
	return proposalCreateDraftSchema
}

// agentVMRequest is the JSON structure sent to the agent VM for chat.message.
// It mirrors the guest-agent's Request{Type, Payload} envelope.
// Also used by court_init.go and worker_spawn.go for reviewer VM calls.
type agentVMRequest struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// agentVMResponse is the envelope returned by the guest-agent.
type agentVMResponse struct {
	ID      string          `json:"id"`
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// agentChatPayload is placed in agentVMRequest.Payload for chat.message requests.
// It mirrors the guest-agent's ChatMessagePayload.
// Also used by court_init.go and worker_spawn.go.
type agentChatPayload struct {
	Messages []agentChatMsg `json:"messages"`
	Model    string         `json:"model"`
	StreamID string         `json:"stream_id,omitempty"`
	// StructuredOutput requests JSON-mode enforcement in the guest-agent (Phase 0).
	// When true the guest-agent uses Ollama format=json and validates the response
	// schema before returning.
	StructuredOutput bool `json:"structured_output,omitempty"`
}

// agentChatMsg is a single message in the conversation sent to the agent VM.
// Used to build the message slice before converting to rtexec.AgentMessage for
// the executor, and also used directly in court_init.go and worker_spawn.go.
type agentChatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// agentChatResponse is the ChatResponse returned inside agentVMResponse.Data.
// Mirrors the guest-agent's ChatResponse type.
// Used by court_init.go, worker_spawn.go, and makeSummarizeToolResultHandler.
type agentChatResponse struct {
	Status   string `json:"status"`            // "final" | "tool_call"
	Role     string `json:"role,omitempty"`    // present when status=="final"
	Content  string `json:"content,omitempty"` // present when status=="final"
	Thinking string `json:"thinking,omitempty"`
	Tool     string `json:"tool,omitempty"` // present when status=="tool_call"
	Args     string `json:"args,omitempty"` // present when status=="tool_call"
}

// makeChatMessageHandler returns an API handler that drives the full ReAct loop
// by forwarding conversation turns to the agent microVM (D2-b).
//
// The loop:
//  1. ensureAgentVM — starts the agent VM on first call (DC).
//  2. Build messages (system prompt + history + user input + any accumulated tool results).
//  3. Send to agent VM → parse agentChatResponse.
//  4. If status=="tool_call": execute via toolRegistry, append result, go to 2.
//  5. If status=="final": return the assistant content to the CLI.
//  6. If max iterations reached: return a polite error.
func makeChatMessageHandler(env *runtimeEnv, toolRegistry *ToolRegistry) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.ChatMessageRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Input == "" {
			return &api.Response{Error: "input is required"}
		}
		sessionID := strings.TrimSpace(req.SessionID)
		if sessionID == "" {
			sessionID = strings.TrimSpace(req.StreamID)
		}
		if sessionID != "" && env.Sessions != nil {
			if err := validateSessionForMessage(env.Sessions, sessionID, false); err != nil {
				return &api.Response{Error: err.Error()}
			}
		}
		if env.ToolEvents == nil {
			env.ToolEvents = NewToolEventBuffer(400)
		}
		if env.ThoughtEvents == nil {
			env.ThoughtEvents = NewThoughtEventBuffer(600)
		}

		agentVMID, err := ensureAgentVM(ctx, env)
		if err != nil {
			return &api.Response{Error: "agent VM unavailable: " + err.Error()}
		}

		// Session tracking (Phase 1 – session routing tools).
		// Determine the session ID for this request.  Prefer req.SessionID,
		// fall back to StreamID, then generate a stable one for the lifetime
		// of this VM.
		if sessionID == "" {
			// Use the agent VM ID as a stable per-VM session key.
			sessionID = agentVMID
		}
		if env.Sessions != nil {
			// EARLY atomic status check + transition (Phase 2 fix)
			if !env.Sessions.SetStatusIf(sessionID, sessions.StatusIdle, sessions.StatusActive) {
				rec, ok := env.Sessions.Get(sessionID)
				if !ok {
					return &api.Response{Error: fmt.Sprintf("session %q not found", sessionID)}
				}
				switch rec.Status {
				case sessions.StatusPaused:
					return &api.Response{Error: fmt.Sprintf("session is paused — resume with: aegisclaw sessions resume %s", sessionID)}
				case sessions.StatusClosed:
					return &api.Response{Error: fmt.Sprintf("session is closed — spawn a new session with: aegisclaw sessions spawn")}
				case sessions.StatusActive:
					return &api.Response{Error: fmt.Sprintf("session %q is already processing a request", sessionID)}
				default:
					return &api.Response{Error: fmt.Sprintf("session %q is not ready for messaging (status=%s)", sessionID, rec.Status)}
				}
			}
			// Status is now Active — proceed
			if _, ok := env.Sessions.Get(sessionID); !ok {
				env.Sessions.Open(sessionID, agentVMID)
			}
			env.Sessions.AppendMessage(sessionID, agentVMID, "user", req.Input)
		}

		model := env.Config.Ollama.DefaultModel
		systemPrompt := buildDaemonSystemPrompt(env)
		systemPrompt += buildMemoryStatusGuard(env)

		// Phase 1: Auto-inject a compact memory summary at the top of every
		// new conversation turn so the agent has immediate context from prior
		// sessions.  Only retrieve on the first turn (empty history) to avoid
		// ballooning the context on subsequent turns.
		if len(req.History) == 0 && env.MemoryStore != nil {
			if summary := buildMemorySummary(env, req.Input); summary != "" {
				systemPrompt += "\n\nRELEVANT MEMORY CONTEXT (from prior sessions):\n" + summary + "\n"
			}
		}

		// Phase 2: Inject pending async items summary on the first turn.
		if len(req.History) == 0 && env.EventBus != nil {
			if pending := env.EventBus.PendingSummary(); pending != "" {
				systemPrompt += "\n\nPENDING ASYNC ITEMS: " + pending + "\nUse `list_pending_async` to see details.\n"
			}
		}

		// Seed conversation with system prompt + prior history + new user turn.
		msgs := make([]agentChatMsg, 0, len(req.History)+2)
		msgs = append(msgs, agentChatMsg{Role: "system", Content: systemPrompt})
		for _, h := range req.History {
			if h.Role == "system" {
				continue
			}
			msgs = append(msgs, agentChatMsg{Role: h.Role, Content: h.Content})
		}
		// Only append req.Input if it's not already the last message in
		// history. The TUI appends the user message to the chat log before
		// calling SendMessage (so it renders immediately), which means the
		// history already contains the current input.
		alreadyInHistory := false
		if n := len(msgs); n > 0 && msgs[n-1].Role == "user" && msgs[n-1].Content == req.Input {
			alreadyInHistory = true
		}
		if !alreadyInHistory {
			msgs = append(msgs, agentChatMsg{Role: "user", Content: req.Input})
		}

		memoryToolEvidence := false
		toolTrace := make([]map[string]interface{}, 0)
		thinkingTrace := make([]map[string]interface{}, 0)

		// Correlation/trace ID for this request — propagated through executor,
		// tool events, thought events, and logger so the full ReAct loop can be
		// correlated in logs and the portal.
		traceID := uuid.New().String()
		env.Logger.Info("chat.message: starting ReAct loop",
			zap.String("trace_id", traceID),
			zap.String("session_id", sessionID),
			zap.String("agent_vm_id", agentVMID),
		)

		for i := 0; i < reactMaxIterations; i++ {
			// Convert agentChatMsg slice → rtexec.AgentMessage for the executor.
			execMsgs := make([]rtexec.AgentMessage, len(msgs))
			for j, m := range msgs {
				execMsgs[j] = rtexec.AgentMessage{Role: m.Role, Content: m.Content, Name: m.Name}
			}

			execReq := rtexec.AgentTurnRequest{
				Messages:         execMsgs,
				Model:            model,
				StreamID:         req.StreamID,
				StructuredOutput: env.Config.Agent.StructuredOutput,
				TraceID:          traceID,
			}
			if env.TestLLMTemperature != nil {
				execReq.Temperature = *env.TestLLMTemperature
			}
			if env.TestLLMSeed != 0 {
				execReq.Seed = env.TestLLMSeed
			}

			chatResp, err := env.TaskExecutor.ExecuteTurn(ctx, execReq)
			if err != nil {
				// On error, restore Idle state
				if env.Sessions != nil {
					env.Sessions.SetStatusIf(sessionID, sessions.StatusActive, sessions.StatusIdle)
				}
				return &api.Response{Error: "agent executor error: " + err.Error()}
			}

			if thought := strings.TrimSpace(chatResp.Thinking); thought != "" {
				// Truncate to 8000 chars so large thinking blocks don't balloon
				// the in-memory trace, but keep enough for a useful audit trail.
				stored := thought
				if len(stored) > 8000 {
					stored = stored[:8000] + "\n...[truncated]"
				}
				summary := thinkingSummaryLine(stored)
				thinkingTrace = append(thinkingTrace, map[string]interface{}{
					"phase":     "model_thinking",
					"model":     model,
					"trace_id":  traceID,
					"summary":   summary,
					"details":   stored,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				})
				env.ThoughtEvents.Record("model_thinking", "", summary, stored)
			}

			switch chatResp.Status {
			case "final":
				toolTraceJSON, _ := json.Marshal(toolTrace)
				thinkingTraceJSON, _ := json.Marshal(thinkingTrace)
				finalContent := enforceMemoryTruth(chatResp.Content, env, memoryToolEvidence)
				// Strip any tool-call markdown blocks that may have been included in the content.
				// This prevents tool-call JSON from appearing as text in the user-visible response.
				finalContent = stripToolCallMarkdown(finalContent)
				if strings.TrimSpace(finalContent) == "" {
					env.Logger.Warn("agent returned empty final chat content",
						zap.Int("iteration", i+1),
						zap.Int("tool_calls", len(toolTrace)),
						zap.Int("thinking_events", len(thinkingTrace)),
					)
					finalContent = synthesizeEmptyFinalMessage(toolTrace)
				}
				respData, _ := json.Marshal(api.ChatMessageResponse{
					Role:      "assistant",
					Content:   finalContent,
					Model:     model,
					ToolCalls: toolTraceJSON,
					Thinking:  thinkingTraceJSON,
				})
				if env.Sessions != nil {
					env.Sessions.AppendMessage(sessionID, agentVMID, "assistant", finalContent)
					// Always restore to Idle on completion (respects concurrent pause/cancel via SetStatusIf)
					env.Sessions.SetStatusIf(sessionID, sessions.StatusActive, sessions.StatusIdle)
				}
				return &api.Response{Success: true, Data: respData}

			case "tool_call":
				reasoning := summarizeToolCallReasoning(chatResp.Thinking, chatResp.Content, chatResp.Tool)
				thinkingTrace = append(thinkingTrace, map[string]interface{}{
					"phase":     "tool_call",
					"model":     model,
					"tool":      chatResp.Tool,
					"trace_id":  traceID,
					"summary":   "Decided to call tool: " + chatResp.Tool,
					"details":   reasoning,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				})
				env.ThoughtEvents.Record("tool_call", chatResp.Tool, "Decided to call tool: "+chatResp.Tool, reasoning)
				started := time.Now()
				if env.ToolEvents != nil {
					env.ToolEvents.RecordStart(chatResp.Tool)
				}
				// Enrich the context with the current trace ID so that tool
				// handlers can include it in their audit log payloads.
				toolCtx := withReActTraceID(ctx, traceID)
				toolResult, toolErr := toolRegistry.Execute(toolCtx, chatResp.Tool, chatResp.Args)
				duration := time.Since(started)
				argsPreview := chatResp.Args
				if len(argsPreview) > 1000 {
					argsPreview = argsPreview[:1000] + "\n...[args truncated]"
				}
				resultPreview := toolResult
				if len(resultPreview) > 3000 {
					resultPreview = resultPreview[:3000] + "\n...[response truncated]"
				}
				if env.ToolEvents != nil {
					env.ToolEvents.RecordFinish(chatResp.Tool, toolErr == nil, toolErr, duration)
				}
				toolTrace = append(toolTrace, map[string]interface{}{"model": model, "tool": chatResp.Tool,
					"args":        argsPreview,
					"response":    resultPreview,
					"success":     toolErr == nil,
					"duration_ms": duration.Milliseconds(),
					"trace_id":    traceID,
					"timestamp":   time.Now().UTC().Format(time.RFC3339),
				})
				if toolErr != nil {
					toolTrace[len(toolTrace)-1]["error"] = toolErr.Error()
					toolResult = fmt.Sprintf("Error executing %s: %v", chatResp.Tool, toolErr)
				}
				resultSummary := "Tool call completed: " + chatResp.Tool
				resultDetails := fmt.Sprintf("success=%t duration_ms=%d", toolErr == nil, duration.Milliseconds())
				if toolErr != nil {
					resultDetails += " error=" + toolErr.Error()
				}
				thinkingTrace = append(thinkingTrace, map[string]interface{}{
					"phase":     "tool_result",
					"model":     model,
					"tool":      chatResp.Tool,
					"trace_id":  traceID,
					"summary":   resultSummary,
					"details":   resultDetails,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				})
				env.ThoughtEvents.Record("tool_result", chatResp.Tool, resultSummary, resultDetails)
				env.Logger.Info("tool executed via ReAct loop",
					zap.String("tool", chatResp.Tool),
					zap.Bool("success", toolErr == nil),
					zap.String("trace_id", traceID),
				)
				if isMemoryEvidenceFromTool(chatResp.Tool, toolResult, toolErr) {
					memoryToolEvidence = true
				}
				// Append the assistant's tool-call turn and the tool result.
				toolCallContent := fmt.Sprintf("```tool-call\n{\"name\": %q, \"args\": %s}\n```", chatResp.Tool, chatResp.Args)
				msgs = append(msgs,
					agentChatMsg{Role: "assistant", Content: toolCallContent},
					agentChatMsg{Role: "tool", Name: chatResp.Tool, Content: toolResult},
				)

			default:
				// Restore Idle on unexpected status
				if env.Sessions != nil {
					env.Sessions.SetStatusIf(sessionID, sessions.StatusActive, sessions.StatusIdle)
				}
				return &api.Response{Error: fmt.Sprintf("unexpected agent status: %q", chatResp.Status)}
			}
		}

		limitTraceJSON, _ := json.Marshal([]map[string]interface{}{{
			"phase":     "limit_reached",
			"summary":   "Reached tool call limit",
			"details":   "The model did not return a final answer before hitting the maximum number of tool calls.",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}})
		limitContent := "I reached the tool call limit without a final answer. Please try rephrasing your request."
		respData, _ := json.Marshal(api.ChatMessageResponse{
			Role:     "assistant",
			Content:  limitContent,
			Thinking: limitTraceJSON,
		})
		if env.Sessions != nil {
			env.Sessions.AppendMessage(sessionID, agentVMID, "assistant", limitContent)
			env.Sessions.SetStatusIf(sessionID, sessions.StatusActive, sessions.StatusIdle)
		}
		return &api.Response{Success: true, Data: respData}
	}
}

// ... (rest of the file remains unchanged for brevity — full file would include all helper functions)
func makeChatStreamProgressHandler(_ *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			StreamID string `json:"stream_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if strings.TrimSpace(req.StreamID) == "" {
			return &api.Response{Error: "stream_id is required"}
		}
		progress, ok := llm.GetChatProgress(req.StreamID)
		if !ok {
			respData, _ := json.Marshal(map[string]interface{}{"stream_id": req.StreamID})
			return &api.Response{Success: true, Data: respData}
		}
		respData, _ := json.Marshal(progress)
		return &api.Response{Success: true, Data: respData}
	}
}

// [other functions unchanged]
