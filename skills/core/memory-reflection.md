# MemoryReflectionSkill Prompt

You are generating **MemoryReflectionSkill** — a combined short/long-term memory + self-reflection skill for SeedClaw agents.

## Purpose
- Store/retrieve key facts, preferences, episodic traces, summaries (short-term: session; long-term: persistent via safe file if allowed)
- Perform self-reflection: review own/others' outputs, identify errors, suggest improvements, store insights
- Enable learning from failures and context retention across interactions
- Act as pre-git archive: automatically store every new skill generation broadcast by CodeSkill (listen for "store" messages with category "generated_skill")
- Support batch retrieval by category or time range for later git commit handoff

## Strict Rules
1. Use only Go stdlib + optional safe local file access (e.g., /tmp or env-specified dir, read-only by default)
2. Memory storage: simple JSON file or in-memory map + periodic flush; no external DBs
3. Reflection: on receiving a message with "reflect" type, critique input/output against criteria (accuracy, completeness, security, logic)
4. Use JSON-lines stdin/stdout messaging envelope
5. Register on startup with {"type":"register","payload":{"name":"MemoryReflectionSkill"}}
6. Commands via payload: "store" (key/value), "retrieve" (key), "reflect" (content + criteria), "summarize" (past interactions)
7. For pre-git: on "store" with "generated_skill" category, save structured payload {skill_name, source_code, prompt_template, binary_hash, llm_model, timestamp, dependencies}
8. Batch retrieve: support "retrieve" with {category: "generated_skill", since: "earliest|timestamp"} → reply with array of all matching
9. File persistence: optional env var MEMORY_PERSIST_DIR (default /tmp/seedclaw-memory); append to skills.jsonl if set
10. Never expose raw memory to untrusted skills; validate requests
11. Timeout operations; run as nobody

## Output JSON Format

Respond **only** with:

```json
{
  "skill_name": "MemoryReflectionSkill",
  "description": "Persistent memory storage/retrieval + self-reflection and critique loop; pre-git skill archive",
  "prompt_template": "You are MemoryReflectionSkill. Store facts, retrieve context, reflect on outputs for improvement.",
  "go_package": "main",
  "source_code": "... complete Go code including main + _test.go ...",
  "binary_name": "memoryreflection",
  "build_flags": ["-trimpath", "-ldflags=-s -w"],
  "tests_included": true,
  "test_command": "go test -v ./..."
}
```

Generate full single-file implementation + basic tests (store/retrieve, reflect on sample output, batch retrieve generated_skills, error cases).
