# Package `internal/ipc` — Inter-Process Communication (AegisHub)

## Purpose
Implements the IPC mesh for AegisClaw. All inter-VM communication is mediated through a single `MessageHub` (AegisHub). No direct VM-to-VM paths exist. Every message is ACL-checked, sender-identity-verified, and optionally audit-logged via the kernel.

## Files

| File | Description |
|---|---|
| `acl.go` | `VMRole`, `ACLPolicy`, `defaultACLPolicy()` — compiled-in permit table |
| `aegishub_test.go` | Integration tests for `MessageHub` lifecycle, routing, ACL, and spoofing prevention |
| `bridge.go` | `Bridge` — translates vsock `ControlMessage` packets into IPC messages; connects kernel control plane to the hub |
| `hub.go` | `MessageHub` — central router: start/stop, skill registration, `RouteMessage`, stats |
| `router.go` | `Router`, `Message`, `DeliveryResult`, `RouteHandler` — low-level routing primitive |
| `router_test.go` | Unit tests for `Router` registration, routing, anti-spoofing, and self-routing prevention |

## Key Abstractions

- **`Router`** — maps VM/skill IDs to `RouteHandler` functions; enforces `msg.From == senderVMID` anti-spoofing; prevents self-routing
- **`MessageHub`** — wraps `Router` with ACL enforcement (`ACLPolicy.Check`) and optional kernel audit logging; `NewMessageHubNoKernel` variant for in-microVM use
- **`ACLPolicy`** — role-based permit table; `agent` can only send `tool.exec`, `chat.message`, `status`; `skill` can only send `tool.result`, `status`
- **`Bridge`** — glue layer enabling VMs to send IPC messages via vsock `ControlMessage` packets rather than a separate network interface
- **`VMRole`** — `agent`, `cli`, `court`, `builder`, `skill`, `hub`, `daemon`

## How It Fits Into the Broader System
AegisHub (`MessageHub` with `RoleHub`) is the first component started at daemon boot and the last stopped. It is tracked in the composition manifest as `ComponentHub`. The `Bridge` connects it to the `kernel.ControlPlane`, enabling vsock-connected VMs to participate in the IPC mesh without a network stack.

## Notable Dependencies
- `internal/kernel`
- `go.uber.org/zap`, `encoding/json`
