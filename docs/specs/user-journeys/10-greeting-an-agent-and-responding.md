# 10 - Greeting an Agent & Observing RAIL Progress (Thinking + Loop Steps Visible)

## Overview
A user (via Web Portal, CLI, or automated E2E test) must be able to greet an agent (start or continue a conversation) and observe the agent's response process in real time. This includes visible **thinking steps**, streamed **agent loop progress** following the **RAIL** principle, and a post-response **review mode** for the full trace of internal steps. This journey establishes the baseline for observable, trustworthy agent interactions and serves as the foundation for more advanced journeys (e.g., time-of-day queries requiring sandbox VMs, news rundowns with web fetch + injection guards).

## User Story
As a developer, tester, or power user, I want to send a simple greeting to an agent so that:
- The agent responds promptly and correctly
- All intermediate reasoning, planning, and loop iterations are visible and streamed (no black box)
- RAIL fast feedback is honored (initial response < 800 ms perceived)
- After the final message, I can review the complete agent loop trace for audit, debugging, or learning

## Success Criteria (Testable)
- First visible feedback (`agent_thinking` or equivalent status) appears within **300–800 ms** of message submission (RAIL **R**esponse)
- Agent loop steps (e.g., plan formulation, memory recall, self-reflection, tool consideration) stream visibly as distinct, timestamped events or cards
- If tools are invoked (future extension: Python/Node sandbox for "what time is it?"), `tool_call` / `tool_result` events appear with timing and sanitized output
- Final `agent_response` streams incrementally as Markdown (safe rendering)
- Post-final-message, a **"Review Agent Trace"** or equivalent UI/CLI affordance exists and displays the full ordered sequence of loop steps, decisions, timings, and (sanitized) context
- E2E test (Playwright) passes end-to-end: greeting → visible RAIL progress → final message → review trace works
- All steps logged to tamper-evident audit trail via Store VM / Court Scribe
- No security regressions: input sanitized; potential injection attempts rejected (future: small guard model or rule-based + audit)
- CLI and Web Portal parity for observability hooks

## Prerequisites for Testing
- Running Host Daemon + AegisHub + at least one Agent Runtime VM (or contract fixture with chat streaming mock)
- LLM backend reachable (Ollama recommended for local dev)
- Web Portal chat UI with SSE/WebSocket streaming per `chat-ui-data-flow.md`
- Stable `data-testid` attributes on chat input, send button, messages container, thinking/progress panel, and review-trace trigger (added/ensured in G2+ UI work)
- For full daemon E2E: `make test-e2e` environment or `AEGIS_E2E_LIVE=1`
- Audit trail queryable (Store VM or `/api/.../audit` endpoints)

## Step-by-Step Flow (for Implementers & Tests)

1. **Initiate Greeting / New or Existing Session**
   - **CLI (headless or interactive)**: `aegis chat --headless "Hello AegisClaw, who are you?"` or target existing `--session <id>`
   - **Web Portal (E2E primary)**: 
     - Navigate to `/` or `/#chat`
     - Ensure chat input visible (`data-testid="message-input"`)
     - Type greeting: "Hello, introduce yourself and confirm you can see this message."
     - Click send (`data-testid="send-button"`)

2. **RAIL Fast Feedback (Response + Action)**
   - Within 300–800 ms: UI shows typing indicator or first `agent_thinking` event rendered as a subtle "Agent is thinking..." or plan card
   - CLI (if streaming mode): immediate status line or first log marker `AGENT_THINKING:session=...`
   - This satisfies **R**esponse (fast) and **A**ction (first visible plan or intent)

3. **Streaming Intermediate Agent Loop Steps (RAIL I + visible progress)**
   - Agent Runtime emits structured events over AegisHub → Web Portal (or CLI stream):
     - `agent_thinking` (with step description, e.g. "Recalling core identity from system prompt", "Evaluating greeting intent", "Checking session context from Memory VM")
     - Optional `memory_lookup` or `context_recall` events
     - If any tool consideration: `tool_call` preview (even if not executed for simple greeting)
     - Loop iteration markers if multi-step ReAct-style reasoning is active (e.g., "Loop iteration 1: ...")
   - **UI Rendering** (per chat-ui-data-flow + web-portal updates):
     - Thinking steps appear in a dedicated, collapsible "Agent Progress" or "Reasoning Trace" panel/sidebar (real-time append)
     - Or inline as expandable thought bubbles / log lines above the final response
     - Timestamps + duration for each step
     - Progress bar or step counter if loop count known
   - **CLI**: `--verbose` or dedicated `aegis chat --stream-progress` shows live steps; or separate `aegis session steps --session=<id>` tail
   - This fulfills **I**ntermediate visibility while agent works

4. **Final Response Streaming (RAIL L + completion)**
   - `agent_response` chunks arrive incrementally
   - UI appends safely rendered Markdown to the message bubble (no full re-render)
   - `content.is_complete: true` signals end
   - User sees complete answer without ever feeling "stuck" (even if total latency is seconds)

5. **Post-Response Review of Full Trace**
   - After `is_complete`, UI automatically or via explicit button shows **"Review full agent trace"** or "Show reasoning steps" affordance (data-testid e.g. `review-trace-button`)
   - Clicking opens modal, drawer, or navigates to `/sessions/<id>/trace` (or equivalent) displaying:
     - Chronological list of all emitted loop events with metadata
     - Expandable details for each (raw event JSON sanitized)
     - Timing breakdown and total duration
     - Link to full audit trail entry in Store/Court
   - **CLI equivalent**: `aegis session trace --session=<id> --format=markdown` or JSON; or `aegis logs --session=<id> --include-thinking`
   - Test asserts the review surface contains expected steps from the greeting interaction

6. **Verification & Observability Hooks**
   - `aegis sessions list --json` shows session with correct status and last activity
   - `aegis vm list --json` confirms Agent Runtime VM health
   - Audit trail query returns entry for the full interaction (including thinking events if policy allows)
   - Log markers: `AGENT_LOOP_START`, `AGENT_STEP:<description>`, `AGENT_RESPONSE_COMPLETE`

## Integration Test Requirements (E2E + Contract)
- **Playwright (e2e/journeys.spec.js)** must cover:
  - Send greeting → assert first thinking visible < 1s timeout
  - Assert intermediate progress elements (thinking steps, loop indicators) become visible and contain expected content
  - Assert final response renders with correct greeting-appropriate content
  - Assert review trace button/element visible post-response
  - Click review → assert trace view/modal populated with ≥3 distinct steps + timings
  - Failure + recovery path: e.g., transient stream error → subsequent greeting recovers with full RAIL + review
- CLI contract tests (`--json`, `--headless`) exercise trace output and session state
- Streaming tests must be resilient to chunked delivery (use `waitForEvent` patterns or polling on testids)
- Future extensions (not in this journey's DoD):
  - "What time of day is it?" → triggers Python/Node.js sandbox VM call (permissions checked, input sanitized)
  - News topic rundown → web resource fetcher + guard (small model or rules to detect/reject prompt injection; all attempts audited)
- All tests cite this spec + `chat-ui-data-flow.md` RAIL requirements + web-portal.md Testability section

## Non-Goals
- Deep multi-turn collaborative task execution (see journey 03)
- Full sandbox VM details or complex tool orchestration (reserved for dedicated future journeys on time queries, web fetch, etc.)
- Persistent session recovery across daemon restarts (covered in recovery-focused tests)
- Advanced guard model implementation for injection detection (outline only; implement in news-rundown journey)

## Related Documents
- `../chat-ui-data-flow.md` (RAIL definition, message JSON schemas, streaming rules, UI rendering)
- `02-starting-new-conversation.md` (baseline session creation; this journey deepens observability)
- `../web-portal.md` and `web-portal-screens.md` (UI surfaces, data-testid conventions)
- `../agent-runtime.md` (internal loop emission points)
- `../event-system.md` and `../observability.md` (event routing for thinking/tool events)
- `../../prd/agent-autonomy.md` and governance docs (audit requirements)
- Future journey sketches: time-of-day (sandbox), news-rundown (web-fetch + sanitization guard)

## Traceability & Audit
Every greeting interaction produces a tamper-evident entry in the Store VM audit trail. Thinking steps and loop events are included at a configurable verbosity level (default: high for dev/test, redacted in production per policy). Court personas can review traces during high-autonomy or anomalous behavior reviews.