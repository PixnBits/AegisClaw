# `internal/api/` — Package Summary

## Overview
Package `api` implements the local IPC layer between the unprivileged AegisClaw CLI and the privileged daemon process. Communication travels over a Unix domain socket (`/run/aegisclaw.sock`) using a thin HTTP/JSON envelope protocol, allowing any local user to connect (socket permissions `0666`) while the daemon runs as root.

## Architecture
- **CLI side** (`Client`) dials the socket for every command, serialises a `Request{Action, Data}` and deserialises the `Response`.
- **Daemon side** (`Server`) registers `Handler` callbacks by action name and dispatches incoming POST `/api` requests. `CallDirect` allows the in-process dashboard to skip the socket round-trip.
- **Shared types** — all request/response structs live in `server.go` so both sides import the same package with no import cycles.

## File Table

| File | Role |
|------|------|
| `client.go` | `Client` type; Unix-socket HTTP transport; `Call`, `Ping` |
| `server.go` | `Server` type; `Handler` interface; all request/response structs; `DefaultSocketPath` |
| `server_test.go` | Unit tests for `CallDirect` panic recovery |

## Key Interfaces & Types
- `Handler` — `func(context.Context, json.RawMessage) *Response`
- `Request` / `Response` — JSON IPC envelopes
- Domain structs: `CourtReview/VoteRequest`, `Skill*/Vault*/Chat*Request`

## Notable Dependencies
- `go.uber.org/zap` (server-side structured logging)
- Standard library `net`, `net/http`, `encoding/json`, `sync`
