# `gateway.go` — Multi-Channel Message Gateway

## Purpose
Implements the host-side message gateway that routes inbound messages from external channels (Telegram, Discord, HTTP webhooks, etc.) to the daemon and returns replies to the originating channel. Inspired by OpenClaw's central Gateway architecture.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `Message` | Normalized message envelope: `ID`, `ChannelID`, `SenderID`, `Text`, `ReceivedAt`, `Metadata` |
| `Channel` | Interface: `ID()`, `Start(ctx, sink)`, `Send(ctx, msg)`, `Healthy()` |
| `ChannelConfig` | Per-channel config: ID, type, enabled, listen addr, shared secret, extra KV pairs |
| `Config` | Gateway-level config: enabled flag + `[]ChannelConfig` |
| `RouteFunc` | `func(ctx, msg) (reply string, err error)` — pluggable routing to the daemon |
| `Gateway` | Holds registered channels, the route function, an inbound `sink` channel, and an error channel |
| `New(route)` | Creates a `Gateway`; channels must be registered before `Start` |
| `Gateway.Register(ch)` | Adds or replaces a channel adapter |
| `Gateway.Start(ctx)` | Launches all channel goroutines and the route loop; blocks until ctx is done |
| `Gateway.dispatch(ctx, msg)` | Truncates oversized messages, calls `RouteFunc`, sends reply back via the originating channel |

## Security Invariants
- All inbound messages are size-capped at 64 KiB (`maxMessageBytes`).
- Channel identity is verified via the shared secret in `ChannelConfig`.
- No direct VM-to-VM communication; gateway is strictly host-side.

## Notable Dependencies
- Standard library: `context`, `fmt`, `sync`, `time`
