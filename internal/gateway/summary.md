# Package `internal/gateway` — Multi-Channel Message Gateway

## Purpose
Implements the host-side multi-channel message gateway (Phase 2, Task 4) that routes inbound messages from external channels to the daemon and returns replies. Inspired by OpenClaw's central Gateway (`ws://127.0.0.1:18789`). The gateway is strictly host-side — no direct VM-to-VM paths exist through it.

## Files

| File | Description |
|---|---|
| `gateway.go` | `Gateway`, `Channel` interface, `Message`, `Config`, `RouteFunc`, dispatch loop |
| `gateway_test.go` | `Gateway` routing tests using `stubChannel` |
| `webhook.go` | `HTTPWebhookChannel` — HTTP POST adapter with request/reply bridging |

## Key Abstractions

- **`Gateway`** — holds registered `Channel` adapters; starts them concurrently; runs a route loop consuming the shared `sink` channel; calls `RouteFunc` per message
- **`Channel`** interface — `ID()`, `Start(ctx, sink)`, `Send(ctx, msg)`, `Healthy()` — any protocol adapter implementing this can be plugged in
- **`Message`** — normalized envelope: `ID`, `ChannelID`, `SenderID`, `Text`, `ReceivedAt`, `Metadata`
- **`HTTPWebhookChannel`** — stateless HTTP POST adapter; each request blocks waiting for the daemon reply (up to 55 s) then returns it as JSON

## Security Invariants
- All inbound messages are size-capped at **64 KiB** before reaching the `RouteFunc`
- Channel identity authenticated via `X-AegisClaw-Secret` header
- Protocol adapters beyond `webhook` require governed skill code — not activatable ad hoc

## How It Fits Into the Broader System
Enabled via `Config.Gateway.Enabled`. Started by the daemon alongside the API socket. The `RouteFunc` is a thin wrapper around the daemon's internal chat/tool dispatch. Channel adapters translate native protocol messages into the `Message` envelope; the gateway core remains channel-agnostic.

## Notable Dependencies
- Standard library: `context`, `net/http`, `sync`, `time`
- `github.com/google/uuid`
