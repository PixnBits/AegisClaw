# `client.go` — API Client

## Purpose
Provides the `Client` type that the AegisClaw CLI uses to communicate with the running daemon process over a Unix domain socket.

## Key Types / Functions

| Symbol | Description |
|--------|-------------|
| `Client` | Holds `socketPath` and an `*http.Client` whose transport dials the Unix socket. |
| `NewClient(socketPath)` | Constructs a `Client` with a custom `DialContext` that routes all HTTP traffic through the given Unix socket. |
| `Call(ctx, action, data)` | Marshals a `Request` envelope (action + JSON payload), POSTs it to `http://aegisclaw/api`, and decodes the `Response`. |
| `Ping(ctx)` | Convenience wrapper around `Call` that sends the `"ping"` action to confirm daemon reachability. |

## How It Fits Into the Broader System
The client is the CLI side of the local IPC layer. The CLI commands (e.g., `aegisclaw skill activate`) construct a `Client`, call `Call` with the appropriate action string, and interpret the returned `Response`. The daemon side is implemented by `Server` in `server.go`.

## Notable Dependencies
- Standard library: `net`, `net/http`, `encoding/json`
- No third-party imports; the Unix-socket trick re-uses the standard `http.Client` transport.
