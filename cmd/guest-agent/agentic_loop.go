package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	hostToolBridgePort     = 1031
	agenticMaxIterations   = 12
	agenticTotalTimeout    = 10 * time.Minute
	agenticPerCallTimeout  = 300 * time.Second
	agenticMaxContextChars = 300000
	agenticMaxTraceArgsLen = 1000
	agenticMaxTraceOutLen  = 3000
)

type hostBridgeRequest struct {
	RequestID string `json:"request_id"`
	Type      string `json:"type"`
	Tool      string `json:"tool,omitempty"`
	Args      string `json:"args,omitempty"`
	Phase     string `json:"phase,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Details   string `json:"details,omitempty"`
}

type hostBridgeResponse struct {
	RequestID string `json:"request_id"`
	Success   bool   `json:"success"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
}

func runAgenticLoop(ctx context.Context, payload ChatMessagePayload) (*ChatResponse, error) {
	loopCtx, cancel := context.WithTimeout(ctx, agenticTotalTimeout)
	defer cancel()

	msgs := append([]ChatMsg(nil), payload.Messages...)
	toolTrace := make([]map[string]interface{}, 0)
	thinkingTrace := make([]map[string]interface{}, 0)

	for i := 0; i < agenticMaxIterations; i++ {
		if estimateContextChars(msgs) > agenticMaxContextChars {
			return nil, fmt.Errorf("conversation context exceeded limit (%d chars)", agenticMaxContextChars)
		}

		stepCtx, stepCancel := context.WithTimeout(loopCtx, agenticPerCallTimeout)
		chatResp, err := runAgenticStep(stepCtx, payload.Model, payload.StreamID, payload.StructuredOutput, msgs)
		stepCancel()
		if err != nil {
			return nil, err
		}

		if thought := strings.TrimSpace(chatResp.Thinking); thought != "" {
			summary := thought
			if idx := strings.Index(summary, "\n"); idx > 0 {
				summary = summary[:idx]
			}
			if len(summary) > 120 {
				summary = summary[:120]
			}
			thinkingTrace = append(thinkingTrace, map[string]interface{}{
				"phase":     "model_thinking",
				"summary":   summary,
				"details":   thought,
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			})
			sendTraceEventToHost("model_thinking", "", summary, thought)
		}

		switch chatResp.Status {
		case "final":
			if strings.TrimSpace(chatResp.Content) == "" {
				summary := "model returned an empty final response; requesting a concise retry"
				details := "status=final content_len=0"
				sendTraceEventToHost("model_empty_final", "", summary, details)
				thinkingTrace = append(thinkingTrace, map[string]interface{}{
					"phase":     "model_empty_final",
					"summary":   summary,
					"details":   details,
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				})

				// Some models occasionally emit an empty final answer despite valid
				// reasoning. Nudge once and continue the loop rather than surfacing
				// a useless blank reply to the user.
				msgs = append(msgs, ChatMsg{
					Role:    "user",
					Content: "Your previous response was empty. Provide a short direct answer to the latest user request.",
				})
				continue
			}

			toolJSON, _ := json.Marshal(toolTrace)
			thinkingJSON, _ := json.Marshal(thinkingTrace)
			return &ChatResponse{
				Status:        "final",
				Role:          "assistant",
				Content:       strings.TrimSpace(chatResp.Content),
				ToolCalls:     toolJSON,
				ThinkingTrace: thinkingJSON,
			}, nil
		case "tool_call":
			started := time.Now()
			toolResult, toolErr := execToolViaHost(loopCtx, chatResp.Tool, chatResp.Args)
			duration := time.Since(started)
			success := toolErr == nil
			if toolErr != nil {
				toolResult = "Error executing " + chatResp.Tool + ": " + toolErr.Error()
			}
			argsPreview := chatResp.Args
			if len(argsPreview) > agenticMaxTraceArgsLen {
				argsPreview = argsPreview[:agenticMaxTraceArgsLen] + "\n...[args truncated]"
			}
			resultPreview := toolResult
			if len(resultPreview) > agenticMaxTraceOutLen {
				resultPreview = resultPreview[:agenticMaxTraceOutLen] + "\n...[response truncated]"
			}
			toolTrace = append(toolTrace, map[string]interface{}{
				"tool":        chatResp.Tool,
				"args":        argsPreview,
				"response":    resultPreview,
				"success":     success,
				"duration_ms": duration.Milliseconds(),
				"timestamp":   time.Now().UTC().Format(time.RFC3339),
			})
			summary := "Tool call completed: " + chatResp.Tool
			details := fmt.Sprintf("success=%t duration_ms=%d", success, duration.Milliseconds())
			sendTraceEventToHost("tool_result", chatResp.Tool, summary, details)
			thinkingTrace = append(thinkingTrace, map[string]interface{}{
				"phase":     "tool_result",
				"tool":      chatResp.Tool,
				"summary":   summary,
				"details":   details,
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			})

			callContent := fmt.Sprintf("```tool-call\n{\"name\": %q, \"args\": %s}\n```", chatResp.Tool, chatResp.Args)
			msgs = append(msgs,
				ChatMsg{Role: "assistant", Content: callContent},
				ChatMsg{Role: "tool", Name: chatResp.Tool, Content: toolResult},
			)
		default:
			return nil, fmt.Errorf("unexpected status %q", chatResp.Status)
		}
	}

	toolJSON, _ := json.Marshal(toolTrace)
	thinkingJSON, _ := json.Marshal(thinkingTrace)
	return &ChatResponse{
		Status:        "final",
		Role:          "assistant",
		Content:       "I reached the tool call limit without a final answer. Please try rephrasing your request.",
		ToolCalls:     toolJSON,
		ThinkingTrace: thinkingJSON,
	}, nil
}

func runAgenticStep(ctx context.Context, model, streamID string, structured bool, msgs []ChatMsg) (*ChatResponse, error) {
	ollamaMsgs := buildOllamaMsgs(msgs)
	if structured {
		const maxAttempts = 2
		working := append([]map[string]string(nil), ollamaMsgs...)
		for attempt := 0; attempt < maxAttempts; attempt++ {
			content, thinking, err := callOllamaWithRetry(ctx, model, streamID, working, "json", nil)
			if err != nil {
				return nil, fmt.Errorf("ollama error: %w", err)
			}
			var reply structuredChatReply
			if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &reply); err == nil {
				switch reply.Status {
				case "tool_call":
					argsStr := "{}"
					if len(reply.Args) > 0 {
						argsStr = string(reply.Args)
					}
					return &ChatResponse{Status: "tool_call", Tool: reply.Tool, Args: argsStr, Thinking: strings.TrimSpace(thinking)}, nil
				case "final":
					return &ChatResponse{Status: "final", Role: "assistant", Content: reply.Content, Thinking: strings.TrimSpace(thinking)}, nil
				}
			}
			if attempt < maxAttempts-1 {
				working = append(working, map[string]string{"role": "user", "content": structuredOutputCorrectionPrompt})
			}
		}
		return nil, fmt.Errorf("structured output enforcement: model did not return valid JSON after retries")
	}

	content, thinking, err := callOllamaWithRetry(ctx, model, streamID, ollamaMsgs, "", nil)
	if err != nil {
		return nil, fmt.Errorf("ollama error: %w", err)
	}
	toolName, argsJSON, hasTool := parseAgentToolCall(content)
	if hasTool {
		return &ChatResponse{Status: "tool_call", Tool: toolName, Args: argsJSON, Thinking: strings.TrimSpace(thinking)}, nil
	}
	return &ChatResponse{Status: "final", Role: "assistant", Content: content, Thinking: strings.TrimSpace(thinking)}, nil
}

func callOllamaWithRetry(ctx context.Context, model, streamID string, messages []map[string]string, format string, options map[string]interface{}) (string, string, error) {
	const maxAttempts = 2
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		content, thinking, err := callOllamaViaProxy(ctx, model, streamID, messages, format, options)
		if err == nil {
			return content, thinking, nil
		}
		lastErr = err
		if !isRetryableProxyReadErr(err) || attempt == maxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return "", "", fmt.Errorf("%w", err)
		case <-time.After(200 * time.Millisecond):
		}
	}
	return "", "", lastErr
}

func isRetryableProxyReadErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "resource temporarily unavailable") {
		return true
	}
	if strings.Contains(msg, "context canceled while retrying after transient vsock error") {
		return true
	}
	if strings.Contains(msg, "read proxy response") && strings.Contains(msg, "vsock-proxy") {
		return true
	}
	return false
}

func estimateContextChars(msgs []ChatMsg) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Role) + len(m.Name) + len(m.Content)
	}
	return total
}

func execToolViaHost(ctx context.Context, tool, args string) (string, error) {
	req := hostBridgeRequest{
		RequestID: fmt.Sprintf("tool-%d", time.Now().UnixNano()),
		Type:      "tool.exec",
		Tool:      tool,
		Args:      args,
	}
	var resp hostBridgeResponse
	if err := callHostBridge(ctx, req, &resp); err != nil {
		return "", err
	}
	if !resp.Success {
		return "", errors.New(resp.Error)
	}
	return resp.Result, nil
}

func sendTraceEventToHost(phase, toolName, summary, details string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req := hostBridgeRequest{
		RequestID: fmt.Sprintf("trace-%d", time.Now().UnixNano()),
		Type:      "trace.event",
		Phase:     phase,
		ToolName:  toolName,
		Summary:   summary,
		Details:   details,
	}
	var resp hostBridgeResponse
	_ = callHostBridge(ctx, req, &resp)
}

func callHostBridge(ctx context.Context, req hostBridgeRequest, resp *hostBridgeResponse) error {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return fmt.Errorf("vsock socket: %w", err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			unix.Close(fd)
			return fmt.Errorf("context already expired")
		}
		tv := unix.NsecToTimeval(remaining.Nanoseconds())
		_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_SNDTIMEO, &tv)
		_ = unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv)
	}

	sa := &unix.SockaddrVM{CID: unix.VMADDR_CID_HOST, Port: hostToolBridgePort}
	if err := unix.Connect(fd, sa); err != nil {
		unix.Close(fd)
		return fmt.Errorf("vsock connect host bridge: %w", err)
	}

	file := os.NewFile(uintptr(fd), "vsock-host-bridge")
	conn := &vsockConn{file: file}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("send host bridge request: %w", err)
	}
	if err := json.NewDecoder(conn).Decode(resp); err != nil {
		return fmt.Errorf("read host bridge response: %w", err)
	}
	return nil
}
