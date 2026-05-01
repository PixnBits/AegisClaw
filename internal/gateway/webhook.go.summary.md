# `webhook.go` — HTTP Webhook Channel Adapter

## Purpose
Implements the `Channel` interface for HTTP POST-based webhooks. Clients POST JSON to the configured address; the gateway processes the message and returns the daemon's reply synchronously in the HTTP response body.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `HTTPWebhookChannel` | Holds `ChannelConfig`, an `*http.Server`, a `ready` signal channel, and a `pending` map for in-flight requests |
| `NewHTTPWebhookChannel(cfg)` | Validates `cfg.Addr` as a `host:port`; returns error on invalid addr |
| `webhookRequest` | Inbound JSON body: `sender_id`, `text`, `metadata` |
| `webhookResponse` | Outbound JSON body: `reply`, `error` |
| `HTTPWebhookChannel.Start(ctx, sink)` | Starts the HTTP server; handles `POST /` — secret check → size cap → JSON decode → message routing → reply wait (55 s timeout) |
| `HTTPWebhookChannel.Send(ctx, msg)` | Delivers a reply to the pending HTTP request identified by `msg.Metadata["_reply_id"]` |
| `HTTPWebhookChannel.Healthy()` | Returns `true` once the server is listening (signalled via `ready` channel close) |

## Design
Each inbound HTTP request creates a per-request `chan string` registered in `pending`. The gateway's dispatch loop calls `Send()` with the reply, writing into this channel. The HTTP handler blocks on the channel (up to 55 s) then returns the reply — keeping the webhook stateless.

## Notable Dependencies
- Standard library: `net/http`, `net`, `encoding/json`, `sync`, `time`
- `github.com/google/uuid`
