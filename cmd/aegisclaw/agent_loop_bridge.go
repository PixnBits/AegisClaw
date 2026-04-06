package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const agentToolBridgePort = 1031

var (
	agentLoopBridgeMu sync.Mutex
	agentLoopBridges  = make(map[string]net.Listener)
)

type agentLoopBridgeRequest struct {
	RequestID string `json:"request_id"`
	Type      string `json:"type"`

	Tool string `json:"tool,omitempty"`
	Args string `json:"args,omitempty"`

	Phase      string `json:"phase,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Details    string `json:"details,omitempty"`
	Success    *bool  `json:"success,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

type agentLoopBridgeResponse struct {
	RequestID string `json:"request_id"`
	Success   bool   `json:"success"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
}

func ensureAgentLoopBridge(env *runtimeEnv, vmID string, toolRegistry *ToolRegistry) error {
	agentLoopBridgeMu.Lock()
	defer agentLoopBridgeMu.Unlock()

	if _, ok := agentLoopBridges[vmID]; ok {
		return nil
	}

	listenPath, err := env.Runtime.VsockCallbackPath(vmID, agentToolBridgePort)
	if err != nil {
		return fmt.Errorf("resolve agent loop bridge callback path: %w", err)
	}
	_ = os.Remove(listenPath)
	ln, err := net.Listen("unix", listenPath)
	if err != nil {
		return fmt.Errorf("listen agent loop bridge %s: %w", listenPath, err)
	}
	agentLoopBridges[vmID] = ln

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				if ne, ok := acceptErr.(net.Error); ok && ne.Temporary() {
					continue
				}
				return
			}
			go handleAgentLoopBridgeConn(env, toolRegistry, conn)
		}
	}()

	env.Logger.Info("agent loop bridge listening", zap.String("vm_id", vmID), zap.String("socket", listenPath), zap.Int("port", agentToolBridgePort))
	return nil
}

func stopAgentLoopBridge(vmID string) {
	agentLoopBridgeMu.Lock()
	defer agentLoopBridgeMu.Unlock()
	ln, ok := agentLoopBridges[vmID]
	if !ok {
		return
	}
	delete(agentLoopBridges, vmID)
	_ = ln.Close()
}

func handleAgentLoopBridgeConn(env *runtimeEnv, toolRegistry *ToolRegistry, conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	var req agentLoopBridgeRequest
	if err := dec.Decode(&req); err != nil {
		_ = enc.Encode(&agentLoopBridgeResponse{Success: false, Error: "invalid bridge request: " + err.Error()})
		return
	}

	resp := &agentLoopBridgeResponse{RequestID: req.RequestID, Success: true}
	switch req.Type {
	case "tool.exec":
		if strings.TrimSpace(req.Tool) == "" {
			resp.Success = false
			resp.Error = "tool is required"
			break
		}
		started := time.Now()
		if env.ToolEvents != nil {
			env.ToolEvents.RecordStart(req.Tool)
		}
		result, err := toolRegistry.Execute(context.Background(), req.Tool, req.Args)
		duration := time.Since(started)
		if env.ToolEvents != nil {
			env.ToolEvents.RecordFinish(req.Tool, err == nil, err, duration)
		}
		if err != nil {
			resp.Success = false
			resp.Error = err.Error()
			break
		}
		resp.Result = result

	case "trace.event":
		phase := strings.TrimSpace(req.Phase)
		if phase == "" {
			phase = "agent_trace"
		}
		summary := strings.TrimSpace(req.Summary)
		if summary == "" {
			summary = strings.TrimSpace(req.Details)
		}
		if env.ThoughtEvents != nil {
			env.ThoughtEvents.Record(phase, req.ToolName, summary, req.Details)
		}
	default:
		resp.Success = false
		resp.Error = "unsupported bridge request type: " + req.Type
	}

	if err := enc.Encode(resp); err != nil {
		env.Logger.Warn("agent loop bridge response encode failed", zap.Error(err))
	}
}
