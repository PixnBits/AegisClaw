# dashboard_daemon.go — cmd/aegisclaw

## Purpose
Starts the optional Portal VM (`aegisclaw-portal` binary, rootfs from `AEGISCLAW_PORTAL_ROOTFS`). Sets up two vsock bridges: an API bridge on port 1030 (portal ↔ daemon) and an edge HTTP proxy on port 18080 (browser ↔ portal).

## Key Types / Functions
- `startDashboard(ctx, env)` — launches the portal microVM, opens vsock listeners on both ports, starts bridge goroutines.
- API bridge (port 1030): JSON `bridgeRequest` messages from the portal are decoded and dispatched to `api.Server.CallDirect`; responses are JSON-encoded back.
- Edge proxy (port 18080): reverse-proxies HTTP from the portal's dashboard HTTP server to the host for browser access.
- Chat calls through the bridge get a 10-minute context deadline.
- Per-connection accept timeout prevents stale bridges.

## System Fit
Enables the web dashboard UI without exposing any host ports; the portal VM is fully isolated and communicates only via vsock.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/api` — `CallDirect`
- `github.com/PixnBits/AegisClaw/internal/sandbox` — VM launcher
