# `hub.go` — Message Hub (AegisHub)

## Purpose
Implements the `MessageHub` — the sole IPC router for the entire system. No direct VM-to-VM communication is permitted; every message passes through the hub, which applies ACL checks, identity verification, audit logging, and delivers the message to the registered handler.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `HubState` | `starting`, `running`, `stopped` |
| `HubStats` | `MessagesRouted`, `MessagesRejected`, `DeliveryErrors`, `StartedAt` |
| `MessageHub` | Central router: `Router`, `Kernel` (optional), `IdentityRegistry`, `ACLPolicy`, stats, mutex |
| `NewMessageHub(kern, logger)` | Production constructor with kernel audit logging |
| `NewMessageHubNoKernel(logger)` | Constructor for in-microVM use where the host kernel singleton is unavailable |
| `MessageHub.Start()` | Transitions to `running`; registers the hub's own route handler |
| `MessageHub.Stop()` | Unregisters hub handler; transitions to `stopped` |
| `MessageHub.RegisterSkill(id, handler)` | Adds a route; logs `skill.register` audit event if kernel is present |
| `MessageHub.UnregisterSkill(id)` | Removes a route |
| `MessageHub.RouteMessage(senderVMID, msg)` | ACL check → identity check → `Router.Route()` → stats update |
| `MessageHub.Stats()` | Returns a snapshot of `HubStats` |

## Role in the System
AegisHub is the architectural centrepiece of the IPC layer. It is the first component started at daemon boot and the last stopped. In production it runs in its own Firecracker microVM registered with `RoleHub`.

## Notable Dependencies
- `internal/kernel`, `internal/ipc` (Router, ACLPolicy, IdentityRegistry)
- `go.uber.org/zap`, `encoding/json`
