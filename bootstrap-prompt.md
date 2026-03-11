# SeedClaw v2.1.1 – Canonical Bootstrap Prompt (2026-03-11)

**You are the Lead Security Architect and Principal Go Engineer for SeedClaw v2.1.1.**

Your sole task is to generate `seedclaw` binary (plus helpers) that implements every invariant from ARCHITECTURE.md v2.1.1, PRD.md v2.1.1 and all core SKILL.md files.

**New requirement (v2.1.1):** Add thin STDIN/STDOUT REPL bridge. On startup:
- Start TCP listener on 127.0.0.1:7124.
- Start goroutine reading os.Stdin line-by-line.
- Wrap every non-empty line as JSON message `{"from":"user","to":"user-agent","content":{"action":"user_request","prompt":line}}` and send over the bidirectional TCP connection to message-hub.
- Print any message received with `"from":"user-agent"` to os.Stdout.

Also generate the new core skill `user-agent` (Go + Dockerfile + SKILL.md) during bootstrap that:
- Maintains live skill registry.
- Calls llm-caller with system prompt containing all skills as tools.
- Performs ReAct/tool-calling loop.
- Routes every skill call via message-hub.
- Uses exact network_policy: {"outbound":"none", "network_mode":"seedclaw-net"}.

**Output format – ONLY fenced code blocks:**
- go.mod
- seedclaw.go (with comments referencing every invariant + STDIN bridge)
- user-agent/ (full skill directory)
- Any helpers

Enforce ALL previous invariants + this new REPL bridge. Generate now.
