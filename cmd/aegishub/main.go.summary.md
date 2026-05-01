# cmd/aegishub/main.go

## Purpose
Entry point and complete implementation of AegisHub — the system IPC router microVM. AegisHub is the sole routing authority for all inter-VM traffic in the AegisClaw platform; no VM may communicate with another VM directly.

## Security Model
- Runs inside a Firecracker microVM with the same isolation guarantees as all AegisClaw components: read-only rootfs, cap-drop ALL, no shared memory, vsock-only external communication.
- The host daemon connects over vsock (AF_VSOCK, CID 2 → port 1024 inside the VM).
- Enforces ACL policy and identity registry before any message is delivered.
- Updates must flow through the Governance Court SDLC with a signed composition manifest.

## Key Types / Functions
- `HubRequest` / `HubResponse` — JSON envelope types with `id`, `type`, `payload`/`data`, `success`, `error`.
- `RegisterVMPayload` / `UnregisterVMPayload` / `RoutePayload` / `RouteResult` — per-operation typed payloads.
- `main()` — creates a `zap` logger, starts `ipc.MessageHub`, listens on vsock port 1024, handles SIGTERM.
- `server.serve(listener)` — accepts connections in a loop; each connection is handled in a goroutine.
- `server.handleConn(conn)` — JSON decode/encode loop; 30-second per-message deadline; 4 MiB payload cap.
- `server.dispatch(req)` — routes to `handleRegisterVM`, `handleUnregisterVM`, `handleRoute`, or `handleStatus`.
- `handleRoute` — delegates to `ipc.MessageHub.RouteMessage`; if the destination is not the hub itself, instructs the daemon to forward the message to the target VM.
- `handleStatus` — returns hub state, routing statistics, and registered routes.
- `listenVsock(port)` — tries `listenAFVsock` first; falls back to TCP for test environments.

## System Fit
First VM launched by `aegisclaw start`. All IPC between VMs flows through this process, making it the critical security boundary for inter-VM communication.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/ipc` — `MessageHub`, `VMRole`, `Message`, `DeliveryResult`
- `go.uber.org/zap` — structured logging
- `vsock_linux.go` — AF_VSOCK listener implementation
