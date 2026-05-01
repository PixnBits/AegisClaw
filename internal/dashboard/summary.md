# Package `internal/dashboard` — Local Web Dashboard

## Purpose
Implements the optional local web dashboard (Phase 4) that gives operators a browser-based view of the daemon: running agents, async tasks, approvals, memory, audit log, skills, git history, and a live chat interface. Built with pure Go `html/template` and no external UI frameworks.

## Files

| File | Description |
|---|---|
| `server.go` | `Server`, `APIClient` interface, route registration, SSE endpoint, template helpers |
| `server_internal_test.go` | White-box tests for unexported helpers and template utility functions |
| `server_test.go` | Black-box HTTP handler tests using `httptest` |

## Key Abstractions

- **`Server`** — HTTP server wrapping a `*http.ServeMux`; communicates with the daemon exclusively via `APIClient`
- **`APIClient`** — interface: `Call(ctx, action, payload)` — decouples the dashboard from the daemon's internal packages
- **SSE (`/events`)** — server-sent events endpoint for real-time dashboard updates without WebSocket complexity

## Registered Routes (25+)
`/`, `/agents`, `/async`, `/memory`, `/approvals`, `/audit`, `/skills`, `/chat`, `/canvas`, `/events`, `/source`, `/workspace`, `/git`, `/health`, and sub-routes.

## How It Fits Into the Broader System
Enabled via `Config.Dashboard.Enabled`. Started by the daemon alongside the API socket server. All data is fetched from the daemon API — the dashboard has no direct access to internal stores, making it safe to expose on `127.0.0.1` without privilege escalation risk.

## Notable Dependencies
- Standard library only: `html/template`, `net/http`, `encoding/json`
