# cmd/aegisportal/main.go

## Purpose
Entry point and complete implementation of AegisPortal — the web dashboard microVM. Runs inside a Firecracker VM, serves a dashboard HTTP server on vsock port 18080 (accessible via the host's edge proxy), and communicates with the host daemon via an API bridge on vsock port 1030.

## Key Types / Functions
- `portalAPIClient` — implements `dashboard.APIClient`; dials the host on vsock port 1030 for each API call.
- `bridgeRequest` — JSON envelope `{ action, payload }` sent over the host API bridge.
- `main()` — mounts essential filesystems (`mountEssentialFS`), starts vsock listener on port 18080, creates `dashboard.New("vsock", &portalAPIClient{})`, serves via `http.Server`, handles SIGTERM with a 5-second shutdown timeout.
- `dialHostAPIBridge(ctx, port)` — opens an AF_VSOCK connection to `VMADDR_CID_HOST:port` with context deadline propagation as SO_SNDTIMEO/SO_RCVTIMEO.
- `mountEssentialFS()` — mounts proc, sysfs (read-only), devtmpfs, tmpfs on /tmp and /run at startup (PID 1 equivalent).
- `listenVsock(port)` — AF_VSOCK listener (inline, not a separate file).
- `vsockConn` / `vsockListener` / `vsockAddr` — net.Conn/Listener/Addr wrappers over raw AF_VSOCK fds.

## System Fit
Launched optionally by `dashboard_daemon.go` in the main daemon. The portal VM has no direct access to host resources; all data flows through the `portalAPIClient` → vsock port 1030 → `api.Server.CallDirect` in the daemon.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/dashboard` — `New`, `APIResponse`, `APIClient`
- `golang.org/x/sys/unix` — AF_VSOCK socket operations
