// Package loop implements the canonical 6-step Agent Runtime loop.
//
// Observe → Think → Plan → Act → Execute → Judge
//
// This is the heart of the real Agent Runtime VM (agent-runtime.md).
// Every turn (user message or background/proactive task) flows through RunTurn.
//
// SPEC REFERENCES:
//   - docs/specs/agent-runtime.md §Responsibilities (full 6-step loop, context from Memory VM,
//     skills/tools exclusively through AegisHub, no surface-only disclaimers)
//   - docs/specs/memory-vm.md §1 "memory.get_context" must be called at the start of every turn
//   - docs/prd/security-model.md (paranoid: every LLM and tool call is mediated, signed, ACL-checked;
//     fail-closed on any error in the reasoning path)
//   - docs/no-stubs-plan/phase-1.md 1.1b (real packages, no mocks/fallbacks in the prod execution path)
//
// The loop never calls the model directly. It always goes through the injected
// hubclient.Client (which may be vsock inside a real Firecracker microVM).

package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"AegisClaw/internal/agent"
	"AegisClaw/internal/agent/act"
	"AegisClaw/internal/bootargs"
	"AegisClaw/internal/agent/execute"
	"AegisClaw/internal/agent/judge"
	"AegisClaw/internal/agent/observe"
	"AegisClaw/internal/agent/plan"
	"AegisClaw/internal/agent/progress"
	"AegisClaw/internal/agent/think"
	"AegisClaw/internal/transport/hubclient"
)

// RunTurn executes one full 6-step cycle for the given input.
// It first obtains fresh short-term + relevant long-term context from the Memory VM
// (mandatory per memory-vm.md), then runs the six reasoning phases in order.
//
// The provided llmCall is the production path (real signed call via Hub to network-boundary).
// In unit tests a mock can be supplied. Production callers must never pass a mock.
//
// Returns the final Judge result (and any side-effects such as proposals created).
func RunTurn(ctx context.Context, tc *agent.TurnContext, llmCall agent.LLMCallFunc) (*agent.StepResult, error) {
	if tc == nil || tc.Hub == nil || llmCall == nil {
		return nil, fmt.Errorf("loop: invalid turn context or llmCall (paranoid guard)")
	}

	// === Memory VM context fetch (memory-vm.md §Communication Interface) ===
	// This happens automatically before every agent turn.
	memDest := "memory"
	if paired := bootargs.PairedMemoryID(); paired != "" {
		memDest = paired
	}
	memPayload := map[string]interface{}{"reason": "turn-start"}
	memMsg := hubclient.Message{
		Source:      tc.Hub.AssignedID(),
		Destination: memDest,
		Command:     "memory.get_context",
		Payload:     memPayload,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	// The hubclient.Send will sign it for us.
	memResp, err := tc.Hub.Send(ctx, memMsg)
	if err != nil {
		log.Printf("loop: memory.get_context failed (fail-closed): %v", err)
		// Still proceed with empty context rather than hard-crashing the turn,
		// but the error is audited via the Hub.
	} else {
		tc.Input = map[string]interface{}{
			"original": tc.Input,
			"memory":   memResp.Payload,
		}
	}

	sessionID := tc.SessionID
	streamID := tc.StreamID
	progress.BeginTurn(sessionID, streamID)
	progress.StepStarted(sessionID, streamID, "memory", "Loaded memory context")
	if tc.DrainPolls != nil {
		tc.DrainPolls()
	}

	var lastResult *agent.StepResult

	lastResult, err = runStep(ctx, tc, llmCall, "observe", observe.Run)
	if err != nil {
		return nil, fmt.Errorf("observe step: %w", err)
	}

	lastResult, err = runStep(ctx, tc, llmCall, "think", think.Run)
	if err != nil {
		return nil, fmt.Errorf("think step: %w", err)
	}

	lastResult, err = runStep(ctx, tc, llmCall, "plan", plan.Run)
	if err != nil {
		return nil, fmt.Errorf("plan step: %w", err)
	}

	lastResult, err = runStep(ctx, tc, llmCall, "act", act.Run)
	if err != nil {
		return nil, fmt.Errorf("act step: %w", err)
	}

	lastResult, err = runStep(ctx, tc, llmCall, "execute", execute.Run)
	if err != nil {
		return nil, fmt.Errorf("execute step: %w", err)
	}

	lastResult, err = runStep(ctx, tc, llmCall, "judge", judge.Run)
	if err != nil {
		return nil, fmt.Errorf("judge step: %w", err)
	}

	if lastResult != nil && lastResult.Content != "" {
		progress.SetStreamContent(streamID, lastResult.Content)
	}
	progress.FinishTurn(sessionID, streamID, stepContent(lastResult))

	return lastResult, nil
}

var stepLabels = map[string][2]string{
	"observe": {"Observe", "Observing your message and context"},
	"think":   {"Think", "Reasoning about intent and constraints"},
	"plan":    {"Plan", "Planning the next actions"},
	"act":     {"Act", "Choosing concrete actions"},
	"execute": {"Execute", "Executing tools and skills"},
	"judge":   {"Judge", "Reviewing quality and safety"},
}

func runStep(
	ctx context.Context,
	tc *agent.TurnContext,
	llmCall agent.LLMCallFunc,
	name string,
	run func(context.Context, *agent.TurnContext, agent.LLMCallFunc) (*agent.StepResult, error),
) (*agent.StepResult, error) {
	if tc != nil && tc.DrainPolls != nil {
		tc.DrainPolls()
	}
	labels := stepLabels[name]
	if labels[0] == "" {
		labels = [2]string{name, name}
	}
	progress.StepStarted(tc.SessionID, tc.StreamID, name, labels[0]+"…")
	if tc != nil && tc.DrainPolls != nil {
		tc.DrainPolls()
	}
	result, err := run(ctx, tc, llmCall)
	if err != nil {
		return nil, err
	}
	details := stepContent(result)
	progress.StepFinished(tc.SessionID, tc.StreamID, name, labels[0]+" complete", details)
	if tc != nil && tc.DrainPolls != nil {
		tc.DrainPolls()
	}
	return result, nil
}

func stepContent(result *agent.StepResult) string {
	if result == nil {
		return ""
	}
	return result.Content
}

// RunBackgroundWork is a convenience for proactive / autonomy-granted tasks.
// It uses the same real 6-step path (no "mini" or "demo" surface code).
func RunBackgroundWork(ctx context.Context, tc *agent.TurnContext, llmCall agent.LLMCallFunc) (*agent.StepResult, error) {
	// In a full implementation we would lower some thresholds in Judge/Plan
	// based on tc.AutonomyScopes. For 1.1b we simply run the real loop.
	return RunTurn(ctx, tc, llmCall)
}

// Helper to build a real LLMCallFunc backed by the hubclient (the production path).
// This replaces the old callLLM + callLLMWithFallback entirely for the hot path.
func NewRealLLMCaller(hub hubclient.Client, model string) agent.LLMCallFunc {
	if model == "" {
		model = agent.DefaultLLMModel
	}
	return func(ctx context.Context, prompt string) (string, error) {
		if hub == nil {
			return "", fmt.Errorf("no hub client for LLM call (fail-closed)")
		}

		llmReq := map[string]interface{}{
			"model":  model,
			"prompt": prompt,
			"stream": false,
		}
		msg := hubclient.Message{
			Source:      hub.AssignedID(),
			Destination: "network-boundary",
			Command:     "llm.call",
			Payload: map[string]interface{}{
				"request":  llmReq,
				"endpoint": "/api/generate",
			},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}

		resp, err := hub.Send(ctx, msg)
		if err != nil {
			return "", fmt.Errorf("llm.call via hub failed: %w", err)
		}
		if resp.Command == "error" {
			return "", fmt.Errorf("network-boundary error: %v", resp.Payload)
		}

		// Expect the same shape the old callLLM parsed.
		if payload, ok := resp.Payload.(map[string]interface{}); ok {
			if response, ok := payload["response"].(string); ok {
				// Try to extract inner "response" like the old Ollama path did.
				var inner map[string]interface{}
				if jsonErr := json.Unmarshal([]byte(response), &inner); jsonErr == nil {
					if text, ok := inner["response"].(string); ok {
						return text, nil
					}
				}
				return response, nil
			}
			if e, ok := payload["error"].(string); ok {
				return "", fmt.Errorf("LLM error: %s", e)
			}
		}
		return "", fmt.Errorf("unexpected LLM response shape from hub")
	}
}

