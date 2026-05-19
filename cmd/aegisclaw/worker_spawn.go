package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/worker"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// spawnWorkerParams are the inputs for the spawn_worker tool.
type spawnWorkerParams struct {
	TaskDescription string   `json:"task_description"`
	Role            string   `json:"role"`   // researcher | coder | summarizer | custom
	ToolsGranted    []string `json:"tools_granted,omitempty"`
	TimeoutMins     int      `json:"timeout_mins,omitempty"`
	TaskID          string   `json:"task_id,omitempty"`
}

// Minimal chat/VM messaging types for worker ReAct loop (stubbed during TCB minimization).
// These were previously provided by an in-process LLM/chat package that has been extracted.
type agentChatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}
type agentChatPayload struct {
	Messages         []agentChatMsg `json:"messages"`
	Model            string         `json:"model"`
	StructuredOutput bool           `json:"structured_output,omitempty"`
}
type agentVMRequest struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}
type agentVMResponse struct {
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}
type agentChatResponse struct {
	Status  string `json:"status"` // final | tool_call | error
	Content string `json:"content,omitempty"`
	Tool    string `json:"tool,omitempty"`
	Args    string `json:"args,omitempty"`
}

// maxTimeoutMultiplier is the hard cap on per-worker timeout expressed as a
// multiple of the configured default. Prevents runaway workers from consuming
// resources for an unbounded duration.
const maxTimeoutMultiplier = 3

// spawnWorker creates, starts, and runs a Worker VM for the given subtask.
// It blocks until the Worker completes (or times out), then returns the result.
// The VM is always destroyed on exit (ephemeral).
//
// This runs synchronously so the Orchestrator's ReAct loop gets an immediate
// result back as a tool observation.  For very long tasks the Orchestrator
// should instead set a timer and check worker_status on wakeup.
func spawnWorker(ctx context.Context, env *runtimeEnv, p spawnWorkerParams) (string, error) {
	if p.TaskDescription == "" {
		return "", fmt.Errorf("spawn_worker requires 'task_description'")
	}
	role := worker.Role(p.Role)
	if role == "" {
		role = worker.RoleCustom
	}
	timeoutMins := p.TimeoutMins
	if timeoutMins <= 0 {
		timeoutMins = worker.RoleDefaultTimeoutMins(role)
	}
	if env.Config != nil && env.Config.Worker.DefaultTimeoutMins > 0 && timeoutMins > env.Config.Worker.DefaultTimeoutMins*maxTimeoutMultiplier {
		timeoutMins = env.Config.Worker.DefaultTimeoutMins * maxTimeoutMultiplier
	}

	// Phase 5: WorkerStore concurrency guard removed from Host Daemon.
	// Long-term owner: Store VM. Routed via AegisHub.
	// (Resource limiting now handled externally.)

	workerID := uuid.New().String()
	now := time.Now().UTC()
	timeoutAt := now.Add(time.Duration(timeoutMins) * time.Minute)

	rec := &worker.WorkerRecord{
		WorkerID:        workerID,
		Role:            role,
		TaskDescription: p.TaskDescription,
		ToolsGranted:    p.ToolsGranted,
		TaskID:          p.TaskID,
		SpawnedBy:       "orchestrator",
		SpawnedAt:       now,
		TimeoutAt:       timeoutAt,
		Status:          worker.StatusSpawning,
	}
	// Phase 5: WorkerStore upsert removed from Host Daemon TCB.
	// Long-term owner: Store VM via AegisHub mediation.

	// Audit: worker spawned.
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"worker_id": workerID, "role": role, "task_id": p.TaskID,
		"timeout_mins": timeoutMins,
	})
	act := kernel.NewAction(kernel.ActionWorkerSpawn, "orchestrator", auditPayload)
	env.Kernel.SignAndLog(act) //nolint:errcheck

	env.Logger.Info("spawning worker VM",
		zap.String("worker_id", workerID),
		zap.String("role", string(role)),
		zap.Int("timeout_mins", timeoutMins),
	)

	// Create and start the worker VM.
	rootfsPath := env.Config.Agent.RootfsPath
	if env.Config.Worker.RootfsPath != "" {
		rootfsPath = env.Config.Worker.RootfsPath
	}
	vmID := generateVMID("worker")
	spec := sandbox.SandboxSpec{
		ID:   vmID,
		Name: "aegisclaw-worker-" + string(role),
		Resources: sandbox.Resources{
			VCPUs:    1,
			MemoryMB: 384,
		},
		NetworkPolicy: sandbox.NetworkPolicy{
			NoNetwork:   true,
			DefaultDeny: true,
		},
		RootfsPath:  rootfsPath,
		KernelPath:  env.Config.Sandbox.KernelImage,
		WorkspaceMB: 128,
	}

	if err := env.Runtime.Create(ctx, spec); err != nil {
		return finishWorker(env, rec, worker.StatusFailed, "", "create VM: "+err.Error())
	}
	if err := env.Runtime.Start(ctx, vmID); err != nil {
		env.Runtime.Delete(ctx, vmID) //nolint:errcheck
		return finishWorker(env, rec, worker.StatusFailed, "", "start VM: "+err.Error())
	}
	rec.VMID = vmID
	rec.Status = worker.StatusRunning
	// Phase 5: WorkerStore upsert removed from Host Daemon TCB.
	// Long-term owner: Store VM via AegisHub mediation.

	// Start LLM proxy for the worker VM.
	// LLMProxy removed from Host Daemon TCB (Phase 3); worker LLM routing now via AegisHub/Store VM.
	vsockPath, err := env.Runtime.VsockPath(vmID)
	if err != nil {
		destroyWorkerVM(ctx, env, vmID, workerID)
		return finishWorker(env, rec, worker.StatusFailed, "", "vsock path: "+err.Error())
	}
	// env.LLMProxy.StartForVM stubbed (non-TCB); worker VM launch continues without direct proxy attachment.
	_ = vsockPath

	// Build the worker's specialized system prompt + task.
	systemPrompt := worker.RolePrompt(role) +
		"\nYour task is:\n" + p.TaskDescription + "\n" +
		"\nComplete the task and return a final structured JSON result."
	if len(p.ToolsGranted) > 0 {
		systemPrompt += "\nYou have access to these tools: " + strings.Join(p.ToolsGranted, ", ")
	}

	model := env.Config.Ollama.DefaultModel
	maxToolCalls := worker.RoleMaxToolCalls(role)

	// Build the ReAct loop context.
	msgs := []agentChatMsg{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: "Execute the assigned task now."},
	}

	// Run the worker's ReAct loop (synchronous, time-boxed).
	workerCtx, workerCancel := context.WithDeadline(ctx, timeoutAt)
	defer workerCancel()

	result := ""
	stepCount := 0
	var loopErr error

	// Build a restricted tool registry for the worker.
	workerRegistry := buildWorkerToolRegistry(env, p.ToolsGranted)

	for i := 0; i < maxToolCalls; i++ {
		select {
		case <-workerCtx.Done():
			destroyWorkerVM(ctx, env, vmID, workerID)
			return finishWorker(env, rec, worker.StatusTimedOut, "", "task timed out after "+fmt.Sprintf("%d", timeoutMins)+" minutes")
		default:
		}

		payloadBytes, _ := json.Marshal(agentChatPayload{
			Messages:         msgs,
			Model:            model,
			StructuredOutput: env.Config.Agent.StructuredOutput,
		})
		vmReq := agentVMRequest{
			ID:      uuid.New().String(),
			Type:    "chat.message",
			Payload: json.RawMessage(payloadBytes),
		}

		raw, err := env.Runtime.SendToVM(workerCtx, vmID, vmReq)
		if err != nil {
			loopErr = err
			break
		}

		var vmResp agentVMResponse
		if err := json.Unmarshal(raw, &vmResp); err != nil {
			loopErr = fmt.Errorf("malformed worker response: %w", err)
			break
		}
		if !vmResp.Success {
			loopErr = fmt.Errorf("worker error: %s", vmResp.Error)
			break
		}

		var chatResp agentChatResponse
		if err := json.Unmarshal(vmResp.Data, &chatResp); err != nil {
			loopErr = fmt.Errorf("malformed chat response: %w", err)
			break
		}
		stepCount++

		switch chatResp.Status {
		case "final":
			result = chatResp.Content
			goto done
		case "tool_call":
			toolResult, toolErr := workerRegistry.Execute(workerCtx, chatResp.Tool, chatResp.Args)
			if toolErr != nil {
				// Sanitize: only expose the error category, not internal details.
				toolResult = fmt.Sprintf("Tool %q returned an error. Check arguments and try again.", chatResp.Tool)
			}
			callContent := fmt.Sprintf("```tool-call\n{\"name\": %q, \"args\": %s}\n```", chatResp.Tool, chatResp.Args)
			msgs = append(msgs,
				agentChatMsg{Role: "assistant", Content: callContent},
				agentChatMsg{Role: "tool", Name: chatResp.Tool, Content: toolResult},
			)
		default:
			loopErr = fmt.Errorf("unexpected status: %q", chatResp.Status)
			goto done
		}
	}

done:
	rec.StepCount = stepCount
	destroyWorkerVM(ctx, env, vmID, workerID)

	if loopErr != nil {
		return finishWorker(env, rec, worker.StatusFailed, "", loopErr.Error())
	}
	return finishWorker(env, rec, worker.StatusDone, result, "")
}

// finishWorker updates the worker record, audits the outcome, stores a memory
// entry, and returns the formatted result string for the Orchestrator.
func finishWorker(env *runtimeEnv, rec *worker.WorkerRecord, status worker.WorkerStatus, result, errMsg string) (string, error) {
	now := time.Now().UTC()
	rec.Status = status
	rec.FinishedAt = &now
	rec.Result = result
	rec.Error = errMsg

	// Phase 5: WorkerStore upsert removed from Host Daemon TCB.
	// Long-term owner: Store VM via AegisHub mediation.

	// Merkle-audit the outcome.
	actionType := kernel.ActionWorkerComplete
	if status == worker.StatusFailed {
		actionType = kernel.ActionWorkerDestroy
	} else if status == worker.StatusTimedOut {
		actionType = kernel.ActionWorkerTimeout
	}
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"worker_id": rec.WorkerID, "status": status, "steps": rec.StepCount,
	})
	act := kernel.NewAction(actionType, "orchestrator", auditPayload)
	env.Kernel.SignAndLog(act) //nolint:errcheck

	// Store a memory entry so future turns recall the outcome.
	if env.MemoryStore != nil {
		memValue := fmt.Sprintf("Worker %s (role=%s task_id=%s) finished with status=%s in %d steps.",
			rec.WorkerID[:8], rec.Role, rec.TaskID, status, rec.StepCount)
		if result != "" {
			memValue += " Result: " + truncate(result, 200)
		}
		if errMsg != "" {
			memValue += " Error: " + truncate(errMsg, 100)
		}
		env.MemoryStore.Store(&memory.MemoryEntry{ //nolint:errcheck
			Key:    "worker.result:" + rec.WorkerID,
			Value:  memValue,
			Tags:   []string{"worker", string(rec.Role), rec.TaskID},
			TaskID: rec.TaskID,
		})
	}

	if status == worker.StatusDone {
		return fmt.Sprintf("Worker completed successfully (steps=%d).\n\nResult:\n%s", rec.StepCount, result), nil
	}
	return "", fmt.Errorf("worker %s: status=%s: %s", rec.WorkerID[:8], status, errMsg)
}

// destroyWorkerVM stops and deletes the worker VM and its LLM proxy.
func destroyWorkerVM(ctx context.Context, env *runtimeEnv, vmID, workerID string) {
	// LLMProxy removed from Host Daemon TCB (Phase 3); worker LLM routing now via AegisHub/Store VM.
	// env.LLMProxy.StopForVM(vmID)
	if err := env.Runtime.Stop(ctx, vmID); err != nil {
		env.Logger.Warn("stop worker VM failed",
			zap.String("vm_id", vmID), zap.Error(err))
	}
	if err := env.Runtime.Delete(ctx, vmID); err != nil {
		env.Logger.Warn("delete worker VM failed",
			zap.String("vm_id", vmID), zap.Error(err))
	}

	auditPayload, _ := json.Marshal(map[string]interface{}{"vm_id": vmID, "worker_id": workerID})
	act := kernel.NewAction(kernel.ActionWorkerDestroy, "daemon", auditPayload)
	env.Kernel.SignAndLog(act) //nolint:errcheck

	env.Logger.Info("worker VM destroyed", zap.String("vm_id", vmID))
}

// buildWorkerToolRegistry returns a restricted ToolRegistry for worker VMs.
// Workers are granted only the tools in allowList; if allowList is empty
// they get the default safe set (memory tools + search_tools).
func buildWorkerToolRegistry(env *runtimeEnv, allowList []string) *ToolRegistry {
	full := buildToolRegistry(env)
	if len(allowList) == 0 {
		// Default: grant only safe read-only / memory tools.
		allowList = []string{
			"retrieve_memory", "store_memory", "list_memories",
			"search_tools",
		}
	}

	allow := make(map[string]bool, len(allowList))
	for _, t := range allowList {
		allow[t] = true
	}

	restricted := &ToolRegistry{env: env}
	for name, meta := range full.meta {
		if allow[name] {
			restricted.Register(name, meta.Description, meta.Handler)
		}
	}
	return restricted
}
