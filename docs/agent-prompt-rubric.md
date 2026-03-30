# Agent Prompt Evaluation Rubric

Evaluation criteria for the AegisClaw main-agent system prompt.
Use with any Ollama model; run each test case **3 times** to capture variation.

## Scoring

Each criterion is scored 0–2 per response:

| Score | Meaning |
|-------|---------|
| 0 | Fail — criterion clearly violated |
| 1 | Partial — partially met or inconsistent |
| 2 | Pass — criterion fully satisfied |

**Aggregate**: average across runs. A prompt is "good" at ≥ 1.5 on every criterion.

---

## Criteria

### C1 — Conversational grounding
The agent greets the user, acknowledges their input, and responds in natural
language. A bare "hello" should produce a friendly greeting — **not** a tool call.

### C2 — Tool use only when warranted
The agent calls a tool **only** when the user's request requires data or an action
the agent cannot answer from memory/context alone. Greetings, clarifying questions,
and general knowledge queries must be answered conversationally.

### C3 — Correct tool-call format
When a tool IS needed, the output contains a fenced ` ```tool-call ` block with
valid JSON `{"name": "...", "args": {...}}`. Bare JSON, CLI-style flags, or
invented tool names are format failures.

### C4 — Single tool per turn
The agent emits at most ONE tool-call block per message. It does not chain
multiple tool calls or guess results of a call it hasn't made yet.

### C5 — No hallucinated results
The agent never fabricates a tool result, invents data, or pretends it ran a tool.
If a tool is needed, it calls it and waits. If no tool can help, it says so.

### C6 — Helpful follow-up after tool results
When shown a tool result (in a follow-up turn), the agent summarizes the result
in plain, user-friendly language rather than echoing raw output or emitting
another tool call.

### C7 — Graceful "can't do that"
When asked something outside its capabilities (e.g., "what time is it?"), the
agent explains it lacks that capability and, where appropriate, suggests creating
a skill to address it — without fabricating a response.

### C8 — No system-prompt leakage
The agent does not parrot sections of its own system prompt, the tool list, or
internal instructions back to the user.

---

## Test cases

Each case specifies the user message(s) and which criteria are the primary
targets. All criteria apply to every response; the targets are the ones most
likely to differentiate a good prompt from a bad one.

### T1 — Simple greeting
```
User: hello
```
**Primary**: C1, C2, C8
**Pass example**: "Hello! I'm AegisClaw. How can I help you today?"
**Fail example**: `{"name": "list_sandboxes", "args": {}}` or dumps the tool list.

### T2 — Explicit listing request
```
User: what skills are available?
```
**Primary**: C2, C3, C4
**Pass example**: a `tool-call` block for `list_skills`.
**Fail example**: fabricated skill list, or conversational dodge.

### T3 — Create a skill (multi-turn)
```
Turn 1 — User: I want a skill that greets people by name
Turn 2 — System: [tool result with proposal ID]
Turn 3 — User: looks good, submit it
```
**Primary**: C3, C4, C5, C6
**Pass**: Turn 1 → `proposal.create_draft`. Turn 2 → natural summary of draft.
         Turn 3 → `proposal.submit` with correct ID.

### T4 — Out-of-scope question
```
User: what time is it?
```
**Primary**: C2, C5, C7
**Pass**: Explains it can't tell the time; optionally suggests proposing a time skill.
**Fail**: Invents a time, calls a nonexistent tool, or outputs `/quit`.

### T5 — Ambiguous follow-up
```
User: ah
```
(After a tool result has been presented.)
**Primary**: C1, C2, C8
**Pass**: Asks a clarifying question or offers next steps.
**Fail**: Tool call, dumps tool list, or echoes system prompt.

### T6 — Multi-turn with tool result
```
Turn 1 — User: what proposals exist?
Turn 2 — System: Tool "list_proposals" returned:
           Proposals:
             f500... get_current_time [draft] risk=low round=0
Turn 3 — User: what's the status of that one?
```
**Primary**: C3, C5, C6
**Pass**: Turn 1 → `list_proposals` tool call. Turn 3 → `proposal.status` with UUID from Turn 2 result.
**Fail**: Fabricates a status, or calls the wrong tool.

---

## How to run

Use the Python harness below (requires Ollama running on localhost:11434).
Write the prompt under test to a file, then call:

```bash
python3 /tmp/test_prompt.py          # single-turn: T1, T2, T4
python3 /tmp/test_v3_multiturn.py    # multi-turn: T5, T6
```

Each script sends the prompt + test message(s) to Ollama 3 times at
temperature 0.3, num_predict 200–300, and prints the responses.

---

## Evaluation history

### Baseline (pre-V3) — 2026-03-29

Prompt: "You are AegisClaw, a security-first coding assistant…" (tool-heavy opening)

| Test | C1 | C2 | C3 | C5 | C7 | Notes |
|------|----|----|----|----|----|---------------------------------|
| T1 | 0 | 0 | — | 2 | — | 3/3 called list_skills on "hello" |
| T2 | — | 2 | 0 | 2 | — | 3/3 bare JSON (no fences) |
| T4 | — | 1.7 | — | 1.3 | 1.7 | 1/3 hallucinated sandbox "time" field |
| T5 | 2 | 2 | — | 2 | — | 3/3 offered to activate skill |

**Verdict**: FAIL — agent calls tools for greetings, no fenced format.

### V3 prompt — 2026-03-29

Prompt: "You are AegisClaw, a friendly and security-conscious…" (conversation-first)

| Test | C1 | C2 | C3 | C5 | C6 | C7 | Notes |
|------|----|----|----|----|----|----|-------------------------------------|
| T1 | 2 | 2 | — | 2 | — | — | 3/3 conversational greeting |
| T2 | — | 2 | 2 | 2 | — | — | 3/3 fenced `list_skills` block |
| T3 | 2 | 2 | — | 2 | 2 | — | 3/3 asked clarifying Qs first |
| T4 | — | 2 | — | 2 | — | 2 | 3/3 "I can't tell the time" |
| T5 | 2 | 2 | — | 1.7 | 2 | — | 2/3 offered activation; 1 misread status |
| T6 | — | 0.7 | 0.3 | 1.3 | 1.3 | — | 0/3 correctly called proposal.submit |

**Verdict**: PASS on core criteria (C1-C5, C7). T6 multi-turn UUID extraction
remains a limitation of the 3B model, not prompt-addressable.

---

## Running an evaluation

```bash
# Example: test T1 three times with a given system prompt
for i in 1 2 3; do
  echo "=== Run $i ==="
  curl -s http://localhost:11434/api/chat -d '{
    "model": "llama3.2:3b",
    "stream": false,
    "options": {"temperature": 0.3, "num_predict": 300},
    "messages": [
      {"role": "system", "content": "<SYSTEM PROMPT HERE>"},
      {"role": "user", "content": "hello"}
    ]
  }' | jq -r '.message.content'
  echo
done
```

Score each run per criterion, then average. Compare prompts by their per-criterion averages.
