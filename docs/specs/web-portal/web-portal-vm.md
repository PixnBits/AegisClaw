# Web Portal VM Specification

## Overview
The Web Portal VM is a dedicated, isolated sandbox that hosts the rich collaborative web interface for AegisClaw. It follows the same strict isolation rules as all other sandboxes in the system.

## Responsibilities
- Serve the static frontend assets (HTML, CSS, JS) and handle dynamic API routes
- Render real-time dashboards, chat interfaces, team workspaces, Court views, and autonomy controls
- Proxy real-time updates (SSE / WebSockets) from AegisHub to the browser
- Act as the user-facing presentation layer only — **never** performs business logic, stores persistent state, or directly accesses secrets

## Non-Responsibilities
- Does **not** hold conversation state (Memory VM)
- Does **not** own persistent data (Store VM)
- Does **not** make outbound network calls (Network Boundary VM)
- Does **not** execute agent reasoning (Agent Runtime VMs)
- Does **not** have direct access to the host filesystem or privileges

## Runtime Characteristics
- **Sandbox Type**: Firecracker microVM (Linux) / Docker sandbox (macOS/Windows)
- **Lifecycle**: Started by Host Daemon on `aegis start`; can be restarted independently
- **Networking**: 
  - Receives inbound HTTP traffic **only** from the Host Daemon (reverse proxy)
  - Exposed to the user on `http://localhost:8080` (configurable via Host Daemon)
  - No direct external network access
- **Communication**: Only via vsock/AegisHub (structured JSON messages)
- **Resources**: Lightweight (target < 512 MB RAM, minimal CPU)

## Security Model
- Runs with **zero** host privileges
- All browser → backend traffic is mediated by Host Daemon → AegisHub
- No secrets are ever loaded into this VM (even temporarily)
- Input from the browser (user actions) is treated as untrusted and validated by AegisHub before forwarding
- Compromise of the Web Portal VM grants no access to other components or the host

## Startup & Readiness
- Host Daemon starts the Web Portal VM during system bootstrap
- Exposes health endpoint (`/health`) for Host Daemon monitoring
- The Host Daemon performs an explicit HTTP /health probe (over vsock for Firecracker or TCP for Docker Sandbox) *before* starting the public reverse proxy on :8080 and emitting `WEB_PORTAL_READY`. This eliminates the post-VM-start race that previously produced 502s for early requests.
- Ready signal sent to AegisHub when the web server is listening internally

## Integration Points
- **Inbound**: HTTP requests proxied by Host Daemon
- **Outbound**: Structured API calls to AegisHub (for sessions, tasks, Court data, etc.)
- **Real-time**: SSE/WebSocket connections proxied through Host Daemon

## Observability
- Logs routed through AegisHub to the central audit trail
- Key events emitted:
  - `WEB_PORTAL_STARTED`
  - `WEB_PORTAL_READY`
  - `WEB_PORTAL_CLIENT_CONNECTED`
  - `WEB_PORTAL_ERROR`

## Testability Requirements
- Must support Playwright E2E tests against `http://localhost:8080`
- All major UI elements must have stable `data-testid` attributes
- Must gracefully handle AegisHub or backend unavailability

## Related Documents
- [./implementation-current.md](./implementation-current.md) — Implementation-current application spec (features, look & feel, API surface)
- [./web-portal.md](./web-portal.md) — Target-state portal specification
- [./sdlc-web-portal.md](./sdlc-web-portal.md) — SDLC visibility (proposal → Court → build → PR → deploy)
- [../host-daemon.md](../host-daemon.md) — Reverse proxy and lifecycle management
- [../aegishub.md](../aegishub.md) — All communication mediation
- [../../architecture.md](../../architecture.md) — Overall sandbox model
- [../../prd/runtime-architecture.md](../../prd/runtime-architecture.md)

## Traceability
**Driven by:**
- User Journeys 1–9 (especially chat, dashboard, team, and Court views)
- Runtime Architecture requirements for dedicated UI sandbox