# Web Portal — Greenfield Implementation Notes

## Stack Choice: React + TypeScript + Vite

**React** was chosen for:
- Mature component model suited to progressive disclosure (reasoning collapse, bottom sheets, policy toggles)
- Excellent testing ecosystem (Vitest, Testing Library, Playwright)
- Strong TypeScript support for sharing contracts with Go backend types
- Team familiarity and long-term maintainability

**Zustand** provides lightweight, auditable client state without the complexity of Redux.

## Architecture

```
web-portal/src/
  contracts/     # TypeScript mirrors of internal/dashboard/contracts
  api/           # REST client (presentation-only, no secrets)
  realtime/      # STOMP-over-WebSocket + SSE fallback
  store/         # Portal + policy state (Zustand)
  lib/           # Pure logic (harness, reasoning, members)
  components/    # Reusable UI (AgentActivitySummary, CompactHarness, ActivityFeed, …)
  views/         # Route-level screens
  styles/        # Design tokens + layout (per design-tokens-and-components.md)
```

Build output: `cmd/web-portal/static/` (served by Go binary + Docker image).

## Alignment with Target Specs

| Spec requirement | Implementation |
|------------------|----------------|
| Progressive reasoning visibility | `lib/reasoning.ts` + `ActivityFeed/FeedItem.tsx` with live/collapsed states |
| Agent Activity Summary | `components/AgentActivitySummary/` on Home (desktop), Channels, Dashboard |
| Policy presets (Progressive / Paranoid / Velocity) | `store/policyStore.ts` + `PolicyPresetToggle` (global + per-channel) |
| Mobile bottom nav + bottom sheets | `BottomNav` + `BottomSheet` for context panel |
| Three-zone desktop layout | `layout.css` grid with collapsible right panel |
| Compact Harness strip → Canvas | `CompactHarness` + pipeline strip linking to Canvas view |
| STOMP targeted subscriptions | `realtime/stompClient.ts` + `topicsForView()` per mount |
| Quick export from proposals | Court view + proposal cards |

## What Was Discarded

The previous vanilla JS SPA (`cmd/web-portal/static/app.js` and `js/*.js`) was replaced entirely. No incremental patching of the dense all-in-one dashboard.

## What Was Preserved

- Go backend contracts (`internal/dashboard/contracts/*`)
- STOMP topic naming and payload shapes
- REST API surface (`/api/*`)
- Vsock bridge patterns (unchanged)
- E2E `data-testid` conventions for contract tests

## Development

```bash
cd web-portal && npm install && npm run dev    # hot reload, proxies to :8080
cd web-portal && npm run build                   # → cmd/web-portal/static/
make build-web-portal                            # from repo root
make test-e2e-contract                           # fixture E2E
cd web-portal && npm test                        # unit tests
```

## Testing Pyramid

- **Unit**: `src/lib/*.test.ts`, `src/contracts/topics.test.ts` — reasoning policy, harness events, STOMP topics
- **Integration**: STOMP client frame parsing (via contract tests in Go + portal realtime E2E)
- **E2E**: Playwright journeys in `e2e/portal-*.spec.js` including mobile, progressive reasoning, policy switching
- **Visual**: Screenshot assertions in `e2e/portal-mobile.spec.js`
- **A11y**: `@axe-core/playwright` available for pipeline integration