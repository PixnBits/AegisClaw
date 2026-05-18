package main

// session_handlers.go — Daemon API handlers for Phase 1 session routing tools.
//
// These handlers back the sessions_list, sessions_history, sessions_send, and
// sessions_spawn tool stubs that were previously registered in tool_registry.go.
// They are wired into the daemon's API server in start.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/sessions"
	"github.com/google/uuid"
)

// sessionsListResponse is the JSON payload returned by sessions.list.
type sessionsListResponse struct {
	Sessions []sessionSummary `json:"sessions"`
	Total    int              `json:"total"`
}

// sessionSummary is a compact description of one session for list views.
type sessionSummary struct {
	ID           string          `json:"id"`
	SandboxID    string          `json:"sandbox_id,omitempty"`
	Status       sessions.Status `json:"status"`
	StartedAt    string          `json:"started_at"`
	LastActiveAt string          `json:"last_active_at"`
	MessageCount int             `json:"message_count"`
}

// makeSessionsListHandler lists all tracked sessions.
//
//	API action: sessions.list
//	Request:    {} (no parameters)
//	Response:   sessionsListResponse
func makeSessionsListHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, _ json.RawMessage) *api.Response {
		if env.Sessions == nil {
			out, _ := json.Marshal(sessionsListResponse{Sessions: []sessionSummary{}})
			return &api.Response{Success: true, Data: out}
		}
		all := env.Sessions.List()
		summaries := make([]sessionSummary, 0, len(all))
		for _, r := range all {
			msgs, _ := env.Sessions.History(r.ID, 0)
			summaries = append(summaries, sessionSummary{
				ID:           r.ID,
				SandboxID:    r.SandboxID,
				Status:       r.Status,
				StartedAt:    r.StartedAt.UTC().Format(time.RFC3339),
				LastActiveAt: r.LastActiveAt.UTC().Format(time.RFC3339),
				MessageCount: len(msgs),
			})
		}
		out, err := json.Marshal(sessionsListResponse{Sessions: summaries, Total: len(summaries)})
		if err != nil {
			return &api.Response{Error: "marshal: " + err.Error()}
		}
		return &api.Response{Success: true, Data: out}
	}
}

// makeSessionsHistoryHandler returns the message log for one session.
//
//	API action: sessions.history
//	Request:    {"session_id": "...", "limit": 50}
//	Response:   {"session_id": "...", "messages": [...], "count": N}
func makeSessionsHistoryHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			SessionID string `json:"session_id"`
			Limit     int    `json:"limit"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if strings.TrimSpace(req.SessionID) == "" {
			return &api.Response{Error: "session_id is required"}
		}
		if env.Sessions == nil {
			return &api.Response{Error: "session store not initialized"}
		}
		msgs, err := env.Sessions.History(req.SessionID, req.Limit)
		if err != nil {
			return &api.Response{Error: err.Error()}
		}
		out, err := json.Marshal(map[string]interface{}{
			"session_id": req.SessionID,
			"messages":   msgs,
			"count":      len(msgs),
		})
		if err != nil {
			return &api.Response{Error: "marshal: " + err.Error()}
		}
		return &api.Response{Success: true, Data: out}
	}
}

// makeSessionsSendHandler sends a message to an existing session's agent VM
// and returns the response.  The target session must already exist (i.e. a
// chat.message request must have been made with that session_id at some point).
//
//	API action: sessions.send
//	Request:    {"session_id": "...", "message": "..."}
//	Response:   {"session_id": "...", "reply": "...", "ok": true}
func makeSessionsSendHandler(env *runtimeEnv, toolRegistry *ToolRegistry) api.Handler {
	// chatHandler is built once here, not per-request, so there is no
	// per-call overhead from constructing the handler closure.
	chatHandler := makeChatMessageHandler(env, toolRegistry)
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req struct {
			SessionID string `json:"session_id"`
			Message   string `json:"message"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		req.Message = strings.TrimSpace(req.Message)
		if req.SessionID == "" {
			return &api.Response{Error: "session_id is required"}
		}
		if req.Message == "" {
			return &api.Response{Error: "message is required"}
		}

		if err := validateSessionForMessage(env.Sessions, req.SessionID, true); err != nil {
			return &api.Response{Error: err.Error()}
		}
		if env.Runtime == nil || env.Config == nil {
			return &api.Response{Error: "sessions.send runtime dependencies not available"}
		}

		// Look up the session to include its history so the agent has context.
		var history []api.ChatHistoryItem
		if env.Sessions != nil {
			msgs, _ := env.Sessions.History(req.SessionID, 50)
			for _, m := range msgs {
				history = append(history, api.ChatHistoryItem{
					Role:    m.Role,
					Content: m.Content,
				})
			}
		}

		innerReq, err := json.Marshal(api.ChatMessageRequest{
			Input:     req.Message,
			History:   history,
			SessionID: req.SessionID,
		})
		if err != nil {
			return &api.Response{Error: "marshal inner request: " + err.Error()}
		}

		resp := chatHandler(ctx, innerReq)
		if resp == nil || !resp.Success {
			errMsg := "unknown error"
			if resp != nil && resp.Error != "" {
				errMsg = resp.Error
			}
			return &api.Response{Error: fmt.Sprintf("sessions.send: chat failed: %s", errMsg)}
		}

		var chatResp api.ChatMessageResponse
		if err := json.Unmarshal(resp.Data, &chatResp); err != nil {
			return &api.Response{Error: "parse chat response: " + err.Error()}
		}

		out, _ := json.Marshal(map[string]interface{}{
			"session_id": req.SessionID,
			"reply":      chatResp.Content,
			"ok":         true,
		})
		return &api.Response{Success: true, Data: out}
	}
}

func validateSessionForMessage(store *sessions.Store, sessionID string, requireExisting bool) error {
	if store == nil {
		return fmt.Errorf("session store not initialized")
	}
	rec, ok := store.Get(sessionID)
	if !ok {
		if requireExisting {
			return fmt.Errorf("session %q not found", sessionID)
		}
		return nil
	}
	switch rec.Status {
	case sessions.StatusPaused:
		return fmt.Errorf("session is paused — resume with: aegisclaw sessions resume %s", sessionID)
	case sessions.StatusClosed:
		return fmt.Errorf("session is closed — spawn a new session with: aegisclaw sessions spawn")
	default:
		return nil
	}
}

func makeSessionsStatusHandler(env *runtimeEnv) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		if req.SessionID == "" {
			return &api.Response{Error: "session_id is required"}
		}
		if env.Sessions == nil {
			return &api.Response{Error: "session store not initialized"}
		}
		rec, ok := env.Sessions.Get(req.SessionID)
		if !ok {
			return &api.Response{Error: fmt.Sprintf("session %q not found", req.SessionID)}
		}
		out, _ := json.Marshal(rec)
		return &api.Response{Success: true, Data: out}
	}
}

func makeSessionsPauseHandler(env *runtimeEnv) api.Handler {
	return makeSessionStatusUpdateHandler(env, sessions.StatusPaused)
}

func makeSessionsResumeHandler(env *runtimeEnv) api.Handler {
	return makeSessionStatusUpdateHandler(env, sessions.StatusIdle)
}

func makeSessionsCancelHandler(env *runtimeEnv) api.Handler {
	return makeSessionStatusUpdateHandler(env, sessions.StatusClosed)
}

func makeSessionStatusUpdateHandler(env *runtimeEnv, status sessions.Status) api.Handler {
	return func(_ context.Context, data json.RawMessage) *api.Response {
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		req.SessionID = strings.TrimSpace(req.SessionID)
		if req.SessionID == "" {
			return &api.Response{Error: "session_id is required"}
		}
		if env.Sessions == nil {
			return &api.Response{Error: "session store not initialized"}
		}
		rec, ok := env.Sessions.Get(req.SessionID)
		if !ok {
			return &api.Response{Error: fmt.Sprintf("session %q not found", req.SessionID)}
		}
		if rec.Status == sessions.StatusClosed && status != sessions.StatusClosed {
			return &api.Response{Error: "session is not paused"}
		}
		env.Sessions.SetStatus(req.SessionID, status)
		return &api.Response{Success: true}
	}
}

// makeSessionsSpawnHandler creates a new isolated chat session and returns its
// session ID.  The new session uses the same shared agent VM as the caller
// (Firecracker VMs are expensive; full per-session VM isolation is opt-in via
// the worker spawning mechanism).
//
//	API action: sessions.spawn
//	Request:    {"task_description": "...", "config": {...}}
//	Response:   {"session_id": "...", "ok": true}
func makeSessionsSpawnHandler(env *runtimeEnv, _ *ToolRegistry) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req struct {
			TaskDescription string                 `json:"task_description"`
			Config          map[string]interface{} `json:"config,omitempty"`
		}
		// Config and task_description are optional; ignore parse errors.
		json.Unmarshal(data, &req) //nolint:errcheck

		newID := uuid.New().String()
		if env.Runtime == nil || env.Config == nil {
			return &api.Response{Error: "sessions.spawn runtime dependencies not available"}
		}

		// Ensure the shared agent VM is running so the new session has
		// something to talk to on first use.
		agentVMID, err := ensureAgentVM(ctx, env)
		if err != nil {
			return &api.Response{Error: "agent VM unavailable: " + err.Error()}
		}

		if env.Sessions != nil {
			env.Sessions.Open(newID, agentVMID)
			// If a task description was provided, record it as context.
			if req.TaskDescription != "" {
				env.Sessions.AppendMessage(newID, agentVMID, "system",
					"Task context for this session: "+req.TaskDescription)
			}
		}

		out, _ := json.Marshal(map[string]interface{}{
			"session_id": newID,
			"sandbox_id": agentVMID,
			"ok":         true,
		})
		return &api.Response{Success: true, Data: out}
	}
}
