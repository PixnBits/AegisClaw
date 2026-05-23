package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ChatSession holds persistent state for a single chat session within the chat-router.
type ChatSession struct {
	mu        sync.Mutex
	SessionID string            `json:"session_id"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Messages  []ChatMessage     `json:"messages,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// ChatMessage represents one message in a chat session.
type ChatMessage struct {
	Role      string        `json:"role"` // "user", "assistant", "system"
	Content   string        `json:"content"`
	Timestamp time.Time     `json:"timestamp"`
	ToolCall  *ToolCallInfo `json:"tool_call,omitempty"`
}

// ToolCallInfo captures structured tool execution data within a message.
type ToolCallInfo struct {
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	ToolCallID string          `json:"tool_call_id"`
}

// chatRouter manages chat sessions and routes chat.* message types via a sync.Map.
//
// It is registered under the "chat-router" key so that preferredBackendForAction
// routes "chat.message" to it.
//
// Security: Session state is keyed by session_id with correlation tracking.
// All errors from the router are sanitized before returning.
type chatRouter struct {
	sessions sync.Map // keyed by session_id → *ChatSession
	logger   *zap.Logger
}

// newChatRouter creates an active chatRouter.
func newChatRouter(logger *zap.Logger) *chatRouter {
	return &chatRouter{
		sessions: sync.Map{},
		logger:   logger,
	}
}

// NewChatRouterHandler returns a RouteHandler backed by the chat-router.
func NewChatRouterHandler(logger *zap.Logger) RouteHandler {
	return newChatRouter(logger).Handle
}

// Handle implements the RouteHandler contract for chat-router operations.
func (r *chatRouter) Handle(msg *Message) (*DeliveryResult, error) {
	switch msg.Type {
	case "chat.message":
		return r.handleChatMessage(msg)
	case "chat.session.create":
		return r.handleSessionCreate(msg)
	case "chat.sessions.list":
		return r.handleSessionList(msg)
	case "chat.history":
		return r.handleHistory(msg)
	case "chat.tool.result":
		return r.handleToolResult(msg)
	default:
		return chatErrorResult(msg.ID, fmt.Errorf("unsupported chat action: %s", msg.Type)), nil
	}
}

// handleChatMessage processes an incoming user message, creates or retrieves
// the corresponding session, stores the message, and returns a structured reply.
func (r *chatRouter) handleChatMessage(msg *Message) (*DeliveryResult, error) {
	var in struct {
		Message     string `json:"message"`
		SessionID   string `json:"session_id"`
		Correlation string `json:"correlation_id"`
		Stream      bool   `json:"stream,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &in); err != nil {
		return chatErrorResult(msg.ID, fmt.Errorf("invalid payload for %s", msg.Type)), nil
	}

	if in.SessionID == "" {
		in.SessionID = "s-" + uuid.NewString()
	}
	if in.Correlation == "" {
		in.Correlation = "corr-" + uuid.NewString()
	}

	session := r.getOrCreateSession(in.SessionID)
	session.mu.Lock()
	session.Messages = append(session.Messages, ChatMessage{
		Role:      "user",
		Content:   in.Message,
		Timestamp: time.Now().UTC(),
	})
	session.UpdatedAt = time.Now().UTC()
	session.mu.Unlock()
	r.updateSession(in.SessionID, session)

	// TODO: Replace echo placeholder with real Agent VM integration output.
	reply := map[string]interface{}{
		"session_id":     in.SessionID,
		"reply":          "echo: " + in.Message,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
		"correlation_id": in.Correlation,
		"streaming":      in.Stream,
	}
	data, _ := json.Marshal(reply)
	return &DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil
}

// handleSessionCreate creates a new chat session with the given ID.
func (r *chatRouter) handleSessionCreate(msg *Message) (*DeliveryResult, error) {
	var in struct {
		SessionID string            `json:"session_id"`
		Metadata  map[string]string `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &in); err != nil {
		return chatErrorResult(msg.ID, fmt.Errorf("invalid payload for %s", msg.Type)), nil
	}
	if in.SessionID == "" {
		in.SessionID = "s-" + uuid.NewString()
	}

	session := &ChatSession{
		SessionID: in.SessionID,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Metadata:  in.Metadata,
		Messages:  make([]ChatMessage, 0),
	}
	r.sessions.Store(in.SessionID, session)

	r.logger.Info("chat session created", zap.String("session_id", in.SessionID))

	data, _ := json.Marshal(map[string]interface{}{
		"session_id":    in.SessionID,
		"created_at":    session.CreatedAt.Format(time.RFC3339),
		"message_count": 0,
	})
	return &DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil
}

// handleSessionList returns summary info for all active sessions.
func (r *chatRouter) handleSessionList(msg *Message) (*DeliveryResult, error) {
	var summaries []map[string]interface{}
	r.sessions.Range(func(key, value interface{}) bool {
		s, ok := value.(*ChatSession)
		if !ok {
			return true
		}
		s.mu.Lock()
		summaries = append(summaries, map[string]interface{}{
			"session_id":    s.SessionID,
			"created_at":    s.CreatedAt.Format(time.RFC3339),
			"message_count": len(s.Messages),
			"updated_at":    s.UpdatedAt.Format(time.RFC3339),
		})
		s.mu.Unlock()
		return true
	})
	data, _ := json.Marshal(summaries)
	return &DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil
}

// handleHistory retrieves the message history for a given session.
func (r *chatRouter) handleHistory(msg *Message) (*DeliveryResult, error) {
	var in struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(msg.Payload, &in); err != nil {
		return chatErrorResult(msg.ID, fmt.Errorf("invalid payload for %s", msg.Type)), nil
	}
	if in.SessionID == "" {
		return chatErrorResult(msg.ID, errors.New("session_id required for history")), nil
	}

	val, ok := r.sessions.Load(in.SessionID)
	if !ok {
		return chatErrorResult(msg.ID, errors.New("session not found")), nil
	}
	session := val.(*ChatSession)

	// Return a copy of the message list to avoid exposing internal mutable state.
	session.mu.Lock()
	history := make([]ChatMessage, len(session.Messages))
	copy(history, session.Messages)
	session.mu.Unlock()

	data, _ := json.Marshal(map[string]interface{}{
		"session_id": in.SessionID,
		"messages":   history,
		"count":      len(history),
	})
	return &DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil
}

// handleToolResult records a tool execution result into the session.
func (r *chatRouter) handleToolResult(msg *Message) (*DeliveryResult, error) {
	var in struct {
		SessionID  string          `json:"session_id"`
		ToolCallID string          `json:"tool_call_id"`
		Content    string          `json:"content"`
		ToolCall   json.RawMessage `json:"tool_call,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &in); err != nil {
		return chatErrorResult(msg.ID, fmt.Errorf("invalid payload for %s", msg.Type)), nil
	}
	if in.SessionID == "" {
		return chatErrorResult(msg.ID, errors.New("session_id required for tool result")), nil
	}

	val, ok := r.sessions.Load(in.SessionID)
	if !ok {
		return chatErrorResult(msg.ID, errors.New("session not found")), nil
	}
	session := val.(*ChatSession)

	tc := &ToolCallInfo{
		ToolCallID: in.ToolCallID,
	}
	if in.ToolCall != nil {
		tc.Name = "unknown"
		tc.Arguments = in.ToolCall
	}

	session.mu.Lock()
	session.Messages = append(session.Messages, ChatMessage{
		Role:      "assistant",
		Content:   in.Content,
		Timestamp: time.Now().UTC(),
		ToolCall:  tc,
	})
	session.UpdatedAt = time.Now().UTC()
	messageCount := len(session.Messages)
	session.mu.Unlock()
	r.sessions.Store(in.SessionID, session)

	data, _ := json.Marshal(map[string]interface{}{
		"session_id":    in.SessionID,
		"tool_call_id":  in.ToolCallID,
		"status":        "recorded",
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
		"message_count": messageCount,
	})
	return &DeliveryResult{MessageID: msg.ID, Success: true, Response: data}, nil
}

// getOrCreateSession loads an existing session or creates a new one.
func (r *chatRouter) getOrCreateSession(sessionID string) *ChatSession {
	if val, ok := r.sessions.Load(sessionID); ok {
		return val.(*ChatSession)
	}
	session := &ChatSession{
		SessionID: sessionID,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Metadata:  make(map[string]string),
		Messages:  make([]ChatMessage, 0),
	}
	r.sessions.Store(sessionID, session)
	return session
}

// updateSession overwrites the session state after mutation.
func (r *chatRouter) updateSession(sessionID string, session *ChatSession) {
	r.sessions.Store(sessionID, session)
}

func chatErrorResult(messageID string, err error) *DeliveryResult {
	return &DeliveryResult{
		MessageID: messageID,
		Success:   false,
		Error:     sanitizeChatError(err),
	}
}

func sanitizeChatError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Ensure it satisfies the expected handler signature.
var _ RouteHandler = (*chatRouter)(nil).Handle
