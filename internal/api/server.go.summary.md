# `server.go` — API Server & Shared Types

## Purpose
Defines the daemon-side Unix-socket HTTP server (`Server`), all shared request/response envelope types, and every domain-specific request/response struct used across the IPC boundary (skills, vault, chat, Court).

## Key Types / Functions

| Symbol | Description |
|--------|-------------|
| `Handler` | `func(ctx, json.RawMessage) *Response` — the callback type for action handlers. |
| `Request` / `Response` | JSON envelopes: `Request{Action, Data}` sent by the CLI; `Response{Success, Error, Data}` returned by the daemon. |
| `Server` | Listens on a Unix socket, owns a `map[string]Handler`, dispatches POST `/api` requests. |
| `NewServer` / `Handle` / `Start` / `Stop` | Lifecycle and registration methods. `Start` removes any stale socket, `chmod 0666`s it, then serves via `http.Serve`. |
| `CallDirect` | Bypasses the socket for in-process callers (e.g., the dashboard). Includes panic recovery. |
| `DefaultSocketPath()` | Returns `"/run/aegisclaw.sock"`. |
| `CourtReviewRequest`, `CourtVoteRequest` | Court workflow payloads. |
| `SkillActivate/Invoke/DeactivateRequest` | Skill lifecycle payloads. |
| `ChatMessageRequest`, `ChatHistoryItem`, `ChatMessageResponse` | D2 chat subsystem payloads with session routing support. |
| `VaultSecretAdd/List/DeleteRequest`, `VaultSecretEntry` | Vault secret management payloads. |

## How It Fits Into the Broader System
`Server` is the single IPC hub between the privileged daemon and the unprivileged CLI (and the dashboard). Every feature area registers its handlers via `Handle`, keeping the transport layer decoupled from business logic.

## Notable Dependencies
- `go.uber.org/zap` for structured logging.
- `runtime/debug` for stack traces in `CallDirect` panic recovery.
- Standard library `net/http`, `sync`, `encoding/json`.
