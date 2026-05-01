# `bridge.go` — Kernel Control Plane ↔ IPC Bridge

## Purpose
Connects the `kernel.ControlPlane` (vsock-based guest VM communication) to the `MessageHub` (IPC router). Translates vsock `ControlMessage` packets into `ipc.Message` envelopes and routes them through the hub.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `Bridge` | Holds a `*MessageHub`, `*kernel.Kernel`, and a `*zap.Logger` |
| `NewBridge(hub, kern, logger)` | Constructor |
| `Bridge.RegisterControlPlaneHandlers()` | Registers two handlers on `kern.ControlPlane()`: `ipc.send` and `ipc.routes` |
| `handleIPCSend(vmID, ctlMsg)` | Unmarshals the payload as an `ipc.Message`, calls `hub.RouteMessage(vmID, msg)`, returns a `ControlResponse` |
| `handleIPCRoutes(vmID, ctlMsg)` | Returns the list of all registered route IDs as a JSON array in `ControlResponse.Data` |

## Flow
```
Guest VM (vsock)
  → kernel.ControlPlane  (unix socket accept loop)
  → Bridge.handleIPCSend
  → MessageHub.RouteMessage  (ACL + sender-identity check)
  → Target skill's RouteHandler
```

## Role in the System
The glue layer that allows VMs to participate in the IPC mesh by sending messages through their vsock connection rather than requiring a separate network interface. Without the bridge, VMs would have no way to communicate with each other.

## Notable Dependencies
- `internal/kernel`, `internal/ipc`
- `go.uber.org/zap`, `encoding/json`
