# tool_registry.go — cmd/aegisclaw

## Purpose
Implements `ToolRegistry` — the daemon's central tool dispatch table. Handlers are registered by name; unknown names matching the `<skill>.<tool>` pattern are dispatched to the appropriate skill VM.

## Key Types / Functions
- `ToolRegistry` — map of `name → handler` with a mutex for concurrent-safe access.
- `Register(name, description, handler)` — adds a tool to the registry.
- `Execute(ctx, tool, argsJSON)` — looks up and calls the handler; for unrecognised `<skill>.<tool>` names, delegates to the skill VM dispatcher.
- Built-in tools registered at startup: `retrieve_memory`, `list_memories`, `compact_memory`, `delete_memory`, `proposal.create_draft`, `proposal.submit`, `worker.spawn`, etc.

## System Fit
Central dispatch for the ReAct tool-call step in `chat_handlers.go`. Skills extend the tool surface without code changes to the registry.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/skill` — VM tool invocation
