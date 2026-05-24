# Teams / Multi-Agent Collaborative Views — Implementation Plan

**Status:** Proposed (May 2026)  
**Owner:** Phase 5 UI completion  
**Related:** 
- Journey 08: Multi-agent / Team Workflows
- `web-portal-screens.md` (Team Workspace wireframe)
- `web-portal.md` (Canvas as current collaborative monitoring view)
- `user-journeys/08-multi-agent-team-workflows.md`
- `cli.md` (team commands)
- Current Canvas implementation in `internal/dashboard/server.go`

## Current State (as of late Phase 5)

- **Canvas (`/canvas`)**: Already delivers a strong real-time collaborative monitoring experience:
  - Grid of live agent cards (status, current task)
  - Per-agent tool-call feed
  - ASCII/text "Agent Interaction Graph"
  - Live tool-call log
  - Powered by existing SSE (`/events` with `tool_start`/`tool_end`/`worker_start`)
  - Data from `worker.list`, `sandbox.list`, `skill.list`

- Web Portal already documents Canvas as "Real-time collaborative monitoring view (Implements monitoring + team concepts from journeys #5/#8)".

- No dedicated **Team session** concept yet:
  - No `team.*` actions wired in the Hub/Store/Agent runtime
  - No "New Team" UI or CLI that creates a named team with roles + shared context
  - No unified "Team Workspace" view beyond the generic Canvas + individual chat sessions
  - Memory sharing for teams is not yet ACL'd or team-scoped

- Legacy wireframe in `web-portal-screens.md` shows a richer "Teams" nav item with goal, status, live team chat, shared context panel, and activity feed.

## Goals (from Journey 08 + specs)

1. Users can start a named multi-agent team with explicit roles.
2. Agents share context safely via Memory VM (permission-checked, no leakage).
3. Agents can delegate/handoff work.
4. User has a unified team view (beyond pure monitoring).
5. Full CLI + Web Portal support.
6. No accidental Court/Builder triggers for normal team collaboration.
7. Playwright coverage for the team dashboard experience.

## Proposed Phased Approach

### Phase A: Foundation (Backend + Hub Contracts) — Medium effort
- Define minimal `team.*` action set on the Hub (allow-list in `acls.yaml`):
  - `team.create` (goal + roles list)
  - `team.list` / `team.get`
  - `team.message` (directed or broadcast)
  - `team.disband`
- Extend Agent Runtime to support "team mode":
  - When spawned as part of a team, receive team_id + role prompt + pointer to shared Memory namespace.
  - Add simple delegation primitive (e.g. `delegate_to_role` tool or message).
- Memory VM: Add team-scoped context buckets or ACLs (e.g. `memory.team.<team_id>.*` keys with role-based read/write).
- Store: Record teams as first-class entities (similar to proposals but lighter — no Court by default).

**Deliverable:** CLI `aegis team new "..." --roles=researcher,analyst` works and spawns coordinated agents that can message each other via Hub.

### Phase B: Rich Team UI on top of Canvas (Portal Layer) — UI focused
- Add "Teams" section or dedicated view (can live under Canvas or as `/teams`).
- Team session creation form (goal + role picker) — thin, delegates to `team.create`.
- Unified Team Dashboard:
  - List of active teams + status
  - Per-team: Goal, members (with roles + live status from Canvas cards), shared context summary
  - Live team activity feed (merged tool/thought events tagged by role)
  - Directed chat input (`@researcher ...` or broadcast)
- Enhance Canvas:
  - Filter/group by `team_id`
  - Show team-level interaction graph (roles as nodes)
  - "Team handoff" visual indicators
- Reuse heavy lifting from existing Canvas SSE + client JS.

**Deliverable:** User can create a team in the portal, see live collaborating agents with role awareness, and send directed messages.

### Phase C: Polish, Permissions, Handoffs, Testing
- Proper permission model for shared Memory (enforced at Memory VM + Hub).
- Clean handoff UX (one agent proposing a handoff that another accepts).
- Team archiving / export.
- Full Playwright coverage for Journey 08 success criteria.
- CLI parity for all major team operations.
- Integration with Autonomy controls (team-level autonomy levels).

## Dependencies & Risks

**Dependencies (must be sequenced or parallelized carefully):**
- Agent runtime team-spawning + role prompts (core to Phase A)
- Memory VM team-scoped / ACL'd context (Phase A)
- Hub `team.*` routing + ACL wildcards (already partially supported via `team.*` placeholder)
- Store team entity persistence

**Risks / TCB Considerations:**
- Shared context must never bypass individual agent skill permissions.
- Team creation must remain a lightweight operation (no Court unless the team itself tries to propose a new skill).
- Canvas is already fairly heavy client-side; adding team state must not bloat the thin portal.

## Recommended First Slice (High Value, Lower Risk)

Start with **Phase B on top of existing Canvas** while the backend team.* work happens in parallel:

1. Add basic team session model in the portal (client-side + thin bridge calls — can be mocked initially).
2. Extend Canvas with `team_id` filtering/grouping and role labels.
3. Add a simple "New Team" button that creates a team session (even if it just groups existing workers for demo purposes).
4. Wire a minimal `team.message` action (broadcast or directed) that goes through the Hub.
5. Add data-testid + basic Playwright smoke for team creation + viewing.

This gives visible progress on the legacy wireframe and Journey 08 without blocking on full backend team spawning.

## Success Criteria for "Teams Done"

- Journey 08 automated tests green (CLI + Playwright).
- User can create a 4-role team, watch them collaborate in the Canvas/Team view, send messages, and see handoffs.
- No permissions leaks in shared Memory.
- All major screens (including the new team views) have stable `data-testid`.

## Open Questions to Resolve Before Starting

- Should teams be first-class in the Store from day one, or can we start with ephemeral team sessions in Memory + Hub?
- How much of the "role" concept lives in the agent prompt vs. explicit Hub metadata?
- Do we want a dedicated `/teams` route or is everything under an enhanced `/canvas` + sidebar sufficient?

---

**Next Action Recommendation**

Once the smoke test is expanded and the plan/roadmaps are updated, the highest-leverage next UI slice is **Phase B** above (Canvas team enhancements + basic team session UI). This builds directly on work already shipped (Canvas + SSE + thin architecture) and delivers visible progress on one of the last major screen gaps.

This plan can live in `docs/implementation-plan/teams-multi-agent-plan.md` and be referenced from the main v2 roadmap.

---

## Progress Update: Phase 5 Teams UI Slice (autonomous continuation)

**Completed (per user direction: success feedback first, then each suggestion in turn):**

- **Dedicated `/teams` page** (higher-level than Canvas): server-rendered list + creation form using thin `team.list` / `team.create` (stub-tolerant, real when Store + ACLs available). Canvas `?team=xxx` filter support wired end-to-end.

- **Success state/message/feedback for creation** (direct response to UX request): 
  - Form now JS-intercepted (json POST for consistency with Canvas bridge).
  - Green success banner (`data-testid="team-create-success"`) on real or demo path: "Team 'X' created successfully!" + prominent "View in Canvas →" (uses returned id), "Refresh list", "Create another" (resets form).
  - Graceful noscript fallback (classic submit still functions).
  - Consistent pattern with theme colors (#3fb950 green success).

- **Team messages / activity surfaced**:
  - Table now has "Msgs" column (renders `len(messages)` from `team.list` response — real append-only data from Store visible immediately).
  - New "Team Messages / Activity" section with send form (`data-testid="send-team-msg-form"`) posting to thin `/api/teams/message` (json, with from/to/text).
  - Dedicated green success feedback banner on send (`data-testid="team-msg-success"`): "Message sent to X (broadcast)" + refresh CTA.
  - Handler + Store already supported append-only; UI now exercises the full round-trip with feedback.

- **Richer per-team "Team Overview Cards"**:
  - Flex grid of cards (`data-testid="team-card"`) under "Team Overview Cards" in the Active Teams section.
  - Each shows: name + id, goal (truncated), member badges with roles (from enhanceWorkersWithTeams), Msgs count, direct "View in Canvas" link.
  - Provides the summary stats + activity hints + handoff CTAs requested; purely thin / server-rendered from bridge data + demo workers.

- **Other polish in slice**: data-testids on new elements (form, success, cards, msg inputs, view links), updated labels (removed stale "(demo)"), payload id always provided on create for better real Store compatibility, one combined script block, build + smoke-ready.

**Findings / Notes:**
- Real Store `team.*` (teams.json persistence) + portal thin handlers work end-to-end when ACLs permit (web-portal + aegis principals). Fallbacks remain robust.
- Member/role viz still relies on client-side `enhanceWorkersWithTeams` (round-robin demo) until Phase A runtime team awareness lands.
- No bloat to thin portal; all via existing `fetchRaw` / `apiClient.Call` + `handleTeam*` patterns.
- Directly advances Journey 08 Success Criteria (create team, send messages, unified view, data-testid) and legacy wireframe spirit without waiting for full backend.

**Status:** This completes the high-value Phase B UI slice on top of Canvas (items 1-5 of Recommended First Slice + the 4 UX suggestions). Next natural: enhance `make smoke` (assert new testids + POST/GET flows), light backend hooks or full Journey 08 E2E Playwright, then Phase C polish/permissions.

All changes followed: spec-first (this plan + web-portal.md + user-journeys/08), thin-only, data-testid everywhere, smoke-as-guard, AGENTS.md sudo rules, logical increments.
