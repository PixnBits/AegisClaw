// Package agent defines the core types and interfaces for the real Agent Runtime VM.
//
// This package (and its subpackages) implement the canonical 6-step loop
// (Observe → Think → Plan → Act → Execute → Judge) that runs inside Firecracker
// microVMs.
//
// SPEC REFERENCES (cited on every type and hot path):
//   - docs/specs/agent-runtime.md §Overview, §Responsibilities, §Communication,
//     §Security & Isolation (paranoid), §Key Interfaces ("agent.loop.step")
//   - docs/specs/memory-vm.md §Communication Interface (memory.get_context at start of every turn)
//   - docs/prd/security-model.md (fail-closed on every LLM/tool/memory call; only through Hub)
//   - docs/no-stubs-plan/phase-1.md 1.1b (real packages, no surface-only disclaimers or mocks in prod path)
//   - AGENTS.md + no-stubs-left-resolution-plan.md (verification-first, atomic commits)
//
// All reasoning steps must go through a hubclient.Client (vsock or unix) for
// LLM calls (to network-boundary) and memory access. No direct network, no
// long-term secrets, no surface fallbacks in the execution path.

package agent

import (
	"context"
	"strings"

	"AegisClaw/internal/transport/hubclient"
	"AegisClaw/internal/agent/skills"
)

// TurnContext carries everything a single 6-step turn needs.
// It is created at the beginning of each user message or background task
// after fetching fresh context from the Memory VM (per memory-vm.md).
type TurnContext struct {
	// Input is the raw user / system / background payload for this turn.
	Input interface{}

	// Hub is the authenticated, signed connection to AegisHub.
	// Every LLM call and memory.* call goes through this (the only allowed path).
	Hub hubclient.Client

	// SkillIndex is the current fast local view of callable tools/skills.
	// Every step must respect it ("only use tools from the available local index").
	SkillIndex *skills.AgentSkillIndex

	// CustomInstructions is the pre-computed prefix from workspace (SOUL + AGENTS + TOOLS).
	// Injected into every reasoning prompt (7.4 / 7.6).
	CustomInstructions string

	// AutonomyScopes (future): when populated, judge/plan steps can use them
	// to decide what may be done without further human input.
	AutonomyScopes []string

	// RevokedScopes: populated from Court decisions (Phase 3).
	// The Agent Runtime must fail-closed on any action that would use these scopes.
	// See agent-runtime.md §Event subscription for court feedback + security-model.md (fail-closed).
	RevokedScopes []string

	// ActiveCourtDecisions carries recent signed Court decisions that affect this agent.
	// Used for immediate respect of revocations and terminations.
	ActiveCourtDecisions []map[string]interface{}

	// Metadata for audit / tracing.
	SessionID string
	TurnID    string
}

// StepResult is the output of one phase of the 6-step loop.
type StepResult struct {
	Phase   string      // "observe", "think", ...
	Content string      // The LLM (or structured) output of the phase
	Actions []ToolCall  // Any concrete tool/skill invocations prepared by the phase
	Notes   interface{} // Optional structured data (e.g. extracted entities)
}

// ToolCall represents a concrete invocation the Execute step may perform via the Hub.
type ToolCall struct {
	SkillID string
	Action  string // e.g. "send_message"
	Args    map[string]interface{}
}

// LLMCallFunc is the abstraction the steps use to obtain model output.
// In production it sends a signed "llm.call" message via the Hub to network-boundary.
// In unit tests it can be a mock that returns canned text without any network.
type LLMCallFunc func(ctx context.Context, prompt string) (string, error)

// MemoryClient is a tiny helper interface for the memory.* commands the loop
// must issue at the start of every turn (memory-vm.md §1).
type MemoryClient interface {
	GetContext(ctx context.Context, reason string) (interface{}, error)
	// Store, Search etc. can be added as the Memory VM implementation matures (Group 1.2+).
}

// DefaultLLMModel is the fallback model when AEGIS_DEFAULT_MODEL is not set.
const DefaultLLMModel = "qwen3-coder:30b"

// IsScopeRevoked is the central fail-closed governance check for the Agent Runtime.
// It must be called before any privileged action (tool execution, scope expansion,
// background work, etc.).
//
// If the requested scope (or any prefix) appears in RevokedScopes (populated from
// real Court decisions via hub), the action is denied.
//
// SPEC: agent-runtime.md §Event subscription for court feedback
//       + §Responsibilities (respect Court decisions immediately)
//       security-model.md (fail-closed on every privileged operation)
//       governance-court.md §Court Process (decisions revoke scopes or terminate)
func IsScopeRevoked(tc *TurnContext, scope string) bool {
	if tc == nil || scope == "" {
		return false
	}
	for _, revoked := range tc.RevokedScopes {
		if revoked == scope || strings.HasPrefix(scope, revoked+".") || strings.HasPrefix(revoked, scope+".") {
			return true
		}
	}
	return false
}
