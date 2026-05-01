# cmd/ — Binary Packages

## Overview
The `cmd/` directory contains the four compiled binaries that make up the AegisClaw platform. Each binary targets a specific role in the Firecracker microVM architecture.

## Binaries

| Directory | Binary | Role |
|-----------|--------|------|
| `cmd/aegisclaw/` | `aegisclaw` | Primary CLI and daemon. Manages the agent lifecycle, Governance Court SDLC, vault, event bus, worker VMs, and the optional web dashboard portal. |
| `cmd/aegishub/` | `aegishub` | System IPC router microVM. The sole routing authority for all inter-VM traffic; enforces ACL policy before delivery. Started first by `aegisclaw start`. |
| `cmd/aegisportal/` | `aegisportal` | Web dashboard microVM. Serves the dashboard UI over vsock; communicates with the host daemon via a vsock API bridge. |
| `cmd/guest-agent/` | `guest-agent` | In-VM agent (PID 1 equivalent). Runs inside every skill, worker, and review microVM; handles command execution, file I/O, secret injection, and tool invocation. |

## Communication Architecture

```
Operator / Browser
     │
     ▼
aegisclaw (host daemon)
     ├─ Unix socket API  ◄── CLI subcommands (stop, status, chat, skill, …)
     ├─ vsock:1024       ──► aegishub VM (IPC routing)
     ├─ vsock:18080      ◄── aegisportal VM (dashboard HTTP)
     ├─ vsock:1030       ◄── aegisportal VM (API bridge)
     └─ vsock:1024       ──► guest-agent (in each skill/worker/review VM)
```

## Deployment Notes
- `aegisclaw` is the only binary users interact with directly.
- `aegishub`, `aegisportal`, and `guest-agent` are embedded in their respective rootfs images and are launched automatically by the daemon.
- All inter-VM communication uses vsock (AF_VSOCK); no VM exposes a TCP port to the host network.

## Per-Package Summaries
- [`cmd/aegisclaw/summary.md`](aegisclaw/summary.md)
- [`cmd/aegishub/summary.md`](aegishub/summary.md)
- [`cmd/aegisportal/summary.md`](aegisportal/summary.md)
- [`cmd/guest-agent/summary.md`](guest-agent/summary.md)
