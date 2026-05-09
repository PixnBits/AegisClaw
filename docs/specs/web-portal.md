# Web Portal Specification

## Overview
The Web Portal is the rich, collaborative interface for AegisClaw.

## Hosting Model
- The Web Portal runs inside its own dedicated **isolated microVM** (Firecracker on Linux, Docker sandbox on macOS/Windows).
- The Host Daemon acts as a reverse proxy:
  - Exposes the portal on `http://localhost:8080` (configurable port)
  - Forwards all traffic to the Web Portal microVM via AegisHub/vsock
  - No direct network exposure unless explicitly enabled

## Connection & Security
- All requests from the browser go through the Host Daemon → AegisHub (never directly to other VMs).
- The portal microVM has no direct host privileges.

## Core Principles
- All interactions must go through AegisHub
- Playwright-friendly for E2E tests (stable selectors, clear loading states)
- Real-time updates via Server-Sent Events / WebSockets (proxied by Host Daemon)
- Local-first and secure by default

## Main Views

1. **Dashboard**
   - System status cards
   - Active sessions & tasks
   - Recent Court decisions
   - Notifications

2. **Chat Interface**
   - Multi-session support
   - Real-time streaming responses
   - Support for `@role` mentions in team chats

3. **Team Workspace**
   - Unified view of all agents in a team

4. **Governance / Court**
   - Proposal submission, decisions, audit explorer

5. **Skills Registry**
   - Browse and propose new skills

6. **Autonomy Manager**
   - Per-session controls with presets

## Technical Requirements
- Runs on `http://localhost:8080` by default
- Responsive + dark mode
- Real-time notifications

## Testability
- All journeys must be executable via Playwright

## Related Documents
- `../user-journeys/` (all 9 journeys)
- `../../prd/user-experience-principles.md`
- `../host-daemon.md`
- `../aegishub.md`