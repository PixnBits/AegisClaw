# `docs/agent-prompts.md` — Summary

## Purpose

Centralises all LLM system prompts, role-specific templates, and few-shot examples for the AegisClaw hierarchical multi-agent system. Every prompt enforces strict structured output, ReAct format, security invariants, and dynamic tool discovery.

## Key Contents

### Orchestrator System Prompt (Main Agent)
- Runs inside a Firecracker microVM as the persistent supervisor agent.
- Strict ReAct format: every response is exactly `Thought: …\nAction: tool_name({…})` OR `Final Answer: …`.
- **Critical rules**: always call `retrieve_memory` first; never bypass isolation; never expose secrets in context or code; always use `request_human_approval` for high-risk actions; use `search_tools` before assuming unavailability; delegate to Workers via `spawn_worker`.
- **Model recommendations**: `qwen2.5-coder:14b-q4_K_M` or `qwen3:32b-q3_K_M` for the Orchestrator.

### Worker Prompts
- Role-specific prompts for Researcher, Coder, Summarizer, and Custom workers.
- Lighter/faster models appropriate for specialised sub-tasks.

### Core Principles (all prompts)
- Paranoid-by-design: never bypass isolation; never expose secrets.
- Dynamic tools only: always call `search_tools` when unsure.
- Memory first: start every task with `retrieve_memory`.
- All new capabilities route through `propose_skill` → Governance Court.
- Auditability: reference task IDs, log decisions, escalate uncertainties.

## Fit in the Broader System

Used by `internal/kernel` and `cmd/guest-agent` to construct system prompts at VM startup. Evaluated against `docs/agent-prompt-rubric.md`. Referenced by `docs/agentic-evolution.md` for the hierarchical architecture context.
