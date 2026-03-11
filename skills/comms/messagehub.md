# MessageHubSkill Prompt

You are generating **MessageHubSkill** — a simple, secure, in-process pub/sub message router for SeedClaw skills.

## Purpose
- Receive JSON-line messages on stdin from other skills
- Route them to the correct destination skill (or broadcast)
- Forward matching messages to the target skill's stdin
- Operate concurrently and safely with no shared mutable state outside channels

## Strict Rules

1. Use only Go stdlib (no external packages).
2. Read from os.Stdin in a loop, parse each line as JSON.
3. Message envelope must be exactly:
   ```json
   {
     "from":      string,
     "to":        string | "*",   // "*" = broadcast to all known skills
     "type":      "request" | "response" | "event" | "error",
     "payload":   object,
     "id":        string,
     "timestamp": string (RFC3339)
   }
   ```
4. Maintain a map of skill names → *os.File (their stdin writer)
5. Skills register themselves by sending a special {"type":"register","payload":{"name":"MySkill"}} message on startup.
6. Use goroutines + channels for fan-out.
7. Graceful shutdown on stdin close or SIGTERM.
8. Log routing events verbosely to stdout (human-readable, no secrets).
9. Never read environment variables or files.

## Output JSON Format

Respond **only** with this JSON:

```json
{
  "skill_name": "MessageHubSkill",
  "description": "In-process pub/sub message router using JSON-lines stdin/stdout",
  "prompt_template": "You are MessageHubSkill. Route messages according to the to field. Register yourself on startup.",
  "go_package": "main",
  "source_code": "... complete Go code ...",
  "binary_name": "messagehub",
  "build_flags": ["-trimpath", "-ldflags=-s -w"],
  "tests_included": true,
  "test_command": "go test -v ./..."
}
```

Generate the full single-file main.go + _test.go content implementing the above. Include basic tests for registration, routing, broadcast, and error cases.
