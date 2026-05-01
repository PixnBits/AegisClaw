# `controlplane.go` — vsock Control Plane

## Purpose
Manages vsock-based communication between the kernel and guest VMs. Each Firecracker VM exposes its vsock as a Unix domain socket on the host; the `ControlPlane` listens on these sockets, dispatches messages to registered handlers, and returns responses.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `ControlMessage` | `Type string`, `Payload json.RawMessage` — JSON message from a guest VM |
| `ControlResponse` | `Success bool`, `Error string`, `Data json.RawMessage` — sent back to the guest |
| `MessageHandler` | `func(vmID string, msg ControlMessage) (*ControlResponse, error)` |
| `ControlPlane` | Holds `handlers`, `listeners` (per-VM), mutex, kernel reference, context |
| `NewControlPlane(kernel, logger)` | Constructor; creates a cancellable context |
| `ControlPlane.RegisterHandler(msgType, handler)` | Registers a handler for a message type |
| `ControlPlane.ListenForVM(vmID, socketPath)` | Removes stale socket, starts a `net.Listen("unix", ...)` and launches `acceptLoop` goroutine |
| `ControlPlane.StopListeningForVM(vmID)` | Closes the listener for a specific VM |
| `ControlPlane.Send(vmID, msg)` | Connects to the VM's socket, sends a message, reads response |
| `ControlPlane.Shutdown()` | Cancels context; closes all listeners |
| `ControlPlane.ActiveListeners()` | Returns count of open listeners |

## Role in the System
The physical communication substrate between the host kernel and all guest VMs. Every tool call from the agent VM and every review result from a court VM arrives here before being dispatched to the appropriate handler.

## Notable Dependencies
- Standard library: `context`, `net`, `os`, `sync`
- `go.uber.org/zap`
