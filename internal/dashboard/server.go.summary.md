# `server.go` — Dashboard HTTP Server

## Purpose
Implements the local web dashboard for AegisClaw Phase 4. It is a pure Go HTTP server using `html/template` with no external frameworks. An SSE endpoint (`/events`) pushes real-time updates to connected browsers. All data is fetched from the daemon via the Unix socket API.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `Server` | HTTP server struct: listen addr, `APIClient`, template func map, `*http.ServeMux` |
| `APIClient` | Interface: `Call(ctx, action, payload)` — abstracts daemon API calls |
| `APIResponse` | Mirrors `api.Response`: `Success`, `Error`, `Data json.RawMessage` |
| `New(addr, client)` | Constructor; registers all routes and sets up template helpers |
| `Server.Start(ctx)` | Starts the HTTP server; graceful shutdown on context cancellation |
| `Server.ServeHTTP(w, r)` | Delegates to the internal mux |

## Registered Routes

`/`, `/agents`, `/async`, `/memory`, `/approvals`, `/approvals/decide`, `/audit`, `/skills`, `/skills/proposals/`, `/settings`, `/chat`, `/chat/send`, `/canvas`, `/events` (SSE), `/source`, `/source/browse`, `/workspace`, `/workspace/edit`, `/git`, `/git/diff`, `/health`

## Template Helpers
`fmtTime`, `truncate`, `join`, `toJSON`, `substr`, `len` (type-safe nil-safe).

## Role in the System
Provides a browser-based operator console. The dashboard is optional (enabled via `Config.Dashboard.Enabled`) and communicates exclusively with the daemon API socket — no direct access to internal packages.

## Notable Dependencies
- Standard library: `html/template`, `net/http`, `encoding/json`
