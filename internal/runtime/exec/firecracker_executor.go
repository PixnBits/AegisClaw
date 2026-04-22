package exec

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// VMRuntime is the minimal interface of sandbox.FirecrackerRuntime that
// FirecrackerTaskExecutor depends on.  Using an interface here lets tests
// provide a lightweight stub without importing the full sandbox package.
type VMRuntime interface {
	SendToVM(ctx context.Context, vmID string, req interface{}) (json.RawMessage, error)
}

// agentVMRequest mirrors the envelope expected by the guest-agent.
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

// agentChatPayload is placed inside agentVMRequest.Payload.
type agentChatPayload struct {
	Messages         []AgentMessage `json:"messages"`
	Model            string         `json:"model"`
	StreamID         string         `json:"stream_id,omitempty"`
	StructuredOutput bool           `json:"structured_output,omitempty"`
	Temperature      *float64       `json:"temperature,omitempty"`
	Seed             int64          `json:"seed,omitempty"`
}

// agentChatResponse mirrors the guest-agent's ChatResponse type.
type agentChatResponse struct {
	Status   string `json:"status"`
	Role     string `json:"role,omitempty"`
	Content  string `json:"content,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	Tool     string `json:"tool,omitempty"`
	Args     string `json:"args,omitempty"`
}

// FirecrackerTaskExecutor implements TaskExecutor by forwarding each turn to a
// running guest-agent microVM over vsock.  This is the production implementation
// and is always compiled (no build tags).
type FirecrackerTaskExecutor struct {
	runtime VMRuntime
	vmID    string
}

// NewFirecrackerTaskExecutor creates an executor that forwards agent turns to
// the Firecracker VM identified by vmID via the supplied runtime.
func NewFirecrackerTaskExecutor(runtime VMRuntime, vmID string) *FirecrackerTaskExecutor {
	return &FirecrackerTaskExecutor{runtime: runtime, vmID: vmID}
}

// ExecuteTurn implements TaskExecutor.
func (e *FirecrackerTaskExecutor) ExecuteTurn(ctx context.Context, req AgentTurnRequest) (AgentTurnResponse, error) {
	var temperature *float64
	if req.Temperature != 0 || req.Seed != 0 {
		temperature = &req.Temperature
	}
	payloadBytes, err := json.Marshal(agentChatPayload{
		Messages:         req.Messages,
		Model:            req.Model,
		StreamID:         req.StreamID,
		StructuredOutput: req.StructuredOutput,
		Temperature:      temperature,
		Seed:             req.Seed,
	})
	if err != nil {
		return AgentTurnResponse{}, fmt.Errorf("firecracker executor: marshal payload: %w", err)
	}

	vmReq := agentVMRequest{
		ID:      uuid.New().String(),
		Type:    "chat.message",
		Payload: json.RawMessage(payloadBytes),
	}

	raw, err := e.runtime.SendToVM(ctx, e.vmID, vmReq)
	if err != nil {
		return AgentTurnResponse{}, fmt.Errorf("firecracker executor: send to VM: %w", err)
	}

	var vmResp agentVMResponse
	if err := json.Unmarshal(raw, &vmResp); err != nil {
		return AgentTurnResponse{}, fmt.Errorf("firecracker executor: unmarshal VM response: %w", err)
	}
	if !vmResp.Success {
		return AgentTurnResponse{}, fmt.Errorf("firecracker executor: agent error: %s", vmResp.Error)
	}

	var chatResp agentChatResponse
	if err := json.Unmarshal(vmResp.Data, &chatResp); err != nil {
		return AgentTurnResponse{}, fmt.Errorf("firecracker executor: unmarshal chat response: %w", err)
	}

	return AgentTurnResponse{
		Status:   chatResp.Status,
		Content:  chatResp.Content,
		Thinking: chatResp.Thinking,
		Tool:     chatResp.Tool,
		Args:     chatResp.Args,
	}, nil
}
