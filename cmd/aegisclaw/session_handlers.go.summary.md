# session_handlers.go — cmd/aegisclaw

## Purpose
Implements daemon API handlers for session management: `sessions.list`, `sessions.history`, `sessions.send`, and `sessions.spawn`.

## Key Types / Functions
- `sessionsListResponse` / `sessionSummary` — JSON shapes for list responses.
- `makeSessionsListHandler(env)` — returns all tracked sessions with compact metadata.
- `makeSessionsHistoryHandler(env)` — returns message history for a specific session ID.
- `makeSessionsSendHandler(env)` — injects a message into an existing session.
- `makeSessionsSpawnHandler(env)` — creates a new isolated session (e.g. for a worker VM task).

## System Fit
Backs the `sessions.*` tool stubs registered in `tool_registry.go`. Enables the agent to query and interact with other running sessions.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/sessions`
- `github.com/PixnBits/AegisClaw/internal/api`
