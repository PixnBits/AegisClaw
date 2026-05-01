# Package: cmd/aegishub

## Overview
`cmd/aegishub` is the AegisHub system IPC router — a purpose-built microVM binary that serves as the **sole routing authority** for all inter-VM traffic in the AegisClaw platform. It runs inside a Firecracker microVM started first by the `aegisclaw start` daemon. No VM may communicate with another VM directly; all messages transit AegisHub.

## Security Model
- Read-only rootfs, cap-drop ALL, no shared memory, vsock-only external communication.
- The host daemon connects on vsock port 1024 (CID 2 inside the VM).
- ACL policy and identity registry are enforced before delivery.
- Updates require a signed composition manifest reviewed by the Governance Court.

## Supported Message Types
| Type | Description |
|------|-------------|
| `hub.register_vm` | Associate a VM ID with its access-control role |
| `hub.unregister_vm` | Remove a VM identity on shutdown |
| `hub.route` | Route an IPC message on behalf of a VM |
| `hub.status` | Return hub health statistics |

## Files

| File | Description |
|------|-------------|
| `main.go` | Complete AegisHub implementation: vsock server, dispatch, IPC routing |
| `vsock_linux.go` | Raw AF_VSOCK listener and connection wrapper (Linux only) |
