# `router.go` — IPC Message Router

## Purpose
Defines the `Message` canonical envelope, `DeliveryResult`, the `RouteHandler` function type, and the `Router` that dispatches messages to registered handlers with sender-identity verification.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `Message` | `ID`, `From`, `To`, `Type`, `Payload json.RawMessage`, `Timestamp` |
| `Message.Validate()` | Returns error if `ID`, `From`, `To`, or `Type` is empty |
| `DeliveryResult` | `MessageID`, `Success`, `Error`, `Response json.RawMessage` |
| `RouteHandler` | `func(*Message) (*DeliveryResult, error)` |
| `Router` | `handlers map[string]RouteHandler` with RW mutex |
| `Router.Register(id, handler)` | Adds a handler; returns error on duplicate or empty ID |
| `Router.Unregister(id)` | Removes a handler |
| `Router.Route(senderVMID, msg)` | Validates message, enforces `msg.From == senderVMID` (anti-spoofing), prevents self-routing, looks up and calls the target handler |
| `Router.RegisteredRoutes()` | Returns all registered IDs |
| `Router.HasRoute(id)` | Fast existence check |

## Security Notes
The `senderVMID` parameter comes from the vsock connection (set by the kernel's accept loop) — not from the message payload — so VMs cannot forge the `From` field.

## Role in the System
The low-level routing primitive used by `MessageHub`. `MessageHub` owns a `Router` instance and uses it after ACL and identity checks have passed.

## Notable Dependencies
- Standard library only: `encoding/json`, `fmt`, `sync`, `time`
