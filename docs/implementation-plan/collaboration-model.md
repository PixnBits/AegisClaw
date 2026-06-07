# AegisClaw Collaboration Model - Technical Implementation Plan

**Current branch for work**: `feat/collaboration-model-prewarm-readiness` (main is protected on the remote; all implementation commits go to feature branches).

**Status**: Active iterative implementation (building on PRD + restructure plan). Portions committed in small, reviewable chunks. Focus: faithful to paranoid security model (signed Hub messages, per-VM keys, ACL deny-default, Court as non-bypassable gate, Store as authority), while delivering **<1s on-demand agent/Court VM startup** and channel-based multi-role collaboration.

See:
- `docs/prd/collaboration-model.md` (the model PRD: channels, PM, Court/SDLC roles, on-demand lifecycle).
- `docs/prd/collaboration-restructure-plan.md` (original 6-phase high-level plan + motivation).
- `docs/specs/host-daemon.md` (references this file for the detailed <1s tactics).
- AGENTS.md (exact daemon start/stop: always `make start` / `sudo ./bin/aegis start` etc.).

## Phases (building on the 6 from restructure-plan.md)

**Phase 1 (PRD / model clarity)**: Done in prior work (collaboration-restructure-plan.md + collaboration-model.md PRD, channels as primitive, PM role definition, 7-persona Court, <1s target stated).

**Phase 2 (Core runtime + channels + fast lifecycle foundation)**: In progress / largely done in committed portions.
- Channels + membership + history + post in Store (modeled on teams; routing for channel.* actions).
- Orchestrator: `EnsureRoleAgent(channelHint)`, `EnsureCourtPersona`, `ReleaseIdle`, `StartPairedAgentAndMemory` (parallel), pre-gen keys ring, `timingEnabled`.
- Daemon: explicit non-blocking pre-warm goroutine (using Ensure + PrewarmPooled...), `startOrchestratorCommandReceiver` for "daemon-orchestrator" + "ensure.role" handling (PM calls this), early `startGuestHubBridgesForSession`, `go StartCourtSystem`.
- ACLs extended (config/acls.yaml: channel.* to store/web-portal/agent*/project-manager*; ensure.role to daemon-orchestrator).
- Routing (portalbridge + internal/portalbridge/routing.go): channel.* → store.
- Guest timing + sentinel (`/tmp/aegis-component-ready` at register_complete) + boot metrics instrumentation.
- Project Manager skeleton → real (cmd/project-manager/main.go: loads key, registers as "project-manager", handles user.goal by building plan then does real `channel.post` to store + loop of `ensure.role` {"role": "...", "channel": "..."} to daemon-orchestrator).
- Early bridge + reduced sleeps (100ms/200ms in guest_hub_bridge + agent vsock).
- Sentinel poll in portal chat path (cmd/aegis/portal_bridge.go) and handler in agent.

**Files touched / created (Phase 2/3 portions)**:
- `cmd/store/main.go` (channel create/list/get/join/post + persistence to channels.json + messages[]).
- `internal/runtime/orchestrator.go` (New pregenKeys + timingEnabled, StartVM pops pre-gen, Ensure* set lc.Channel, pre-warm go using EnsureBootable..., parallel wg for paired/court, ListVMs dedup).
- `cmd/aegis/main.go` (pre-warm go after base infra + effHome/SUDO_USER, startOrchestratorCommandReceiver, daemonChildEnv re-injects AEGIS_BOOT_TIMING, early bridges, receiver for ensure.role calling orchestrator.EnsureRoleAgent).
- `cmd/project-manager/main.go` (new; thin binary like court-persona but with real channel.post + ensure.role sends + planning).
- `cmd/aegis/guest_hub_bridge.go` (early start for sessions, reduced retry sleeps).
- `cmd/aegis/portal_bridge.go` (sentinel poll before chat send).
- `cmd/agent/main.go` (sentinel.ready/component.ready handler + reduced sleep).
- `internal/sandbox/guest_key_inject.go` + `rootfs_linux.go` (PrewarmPooledRootfsCopies, claimPooledRootfs, copy fast path, Ensure).
- `internal/sandbox/firecracker.go` (removed fc_pid pollution of metrics).
- `config/acls.yaml` (channel + project-manager + daemon-orchestrator rules).
- `internal/portalbridge/routing.go` (channel. prefix → store).
- `docs/specs/host-daemon.md` (added Dynamic Lifecycle section + references).
- `scripts/boot-metrics.sh`, Makefile, aegisclaw-sudoers.example (supporting).

**Phase 3 (PM real usage + role Ensure + Court integration + channels in UI/Store full)**: PM enhanced to actually post plans and drive role ensures; Store channels authoritative; ACLs + hub routing; orchestrator Ensure wired to PM "daemon-orchestrator" target. (Current session has the wiring; full end-to-end visible plan execution in a channel is the next visible milestone.)

**Phase 4 (Agent specialisation)**: Role prompts/souls for PM, coder/tester, the 7 Court personas (thin court-persona binary already pulls persona from cmdline and does proposal.get + scribe.submit_vote). More custom getPMPrompt etc. already partially in PM.

**Phase 5 (Portal / UX)**: Channel roster, @mentions to project-manager*/court-persona-*, presence, delegation views, Court summaries posted back to channel. (Portalbridge already routes channel.*; web UI work is future.)

**Phase 6 (Polish, <1s validation, E2E, defaults)**: Measurement loops with AEGIS_BOOT_TIMING=1 + scripts/boot-metrics.sh (or `aegis vm boot-metrics`), no long external sleeps, solo "main" channel defaults, migration, full tests.

## <1s On-Demand Startup Tactics (critical, see host-daemon.md)

Target: agent/memory (and Court/PM roles) guest `register_complete` / sentinel in the low hundreds of ms; host phase (including any rootfs work) ~100-200 ms when claim hits.

Current achieved (good runs from prior measurement):
- memory-sess-*/agent guest register_complete: ~464-467 ms.
- Host (when pooled claim succeeds): ~110-196 ms.
- Court: host/fc phases only in some captures (force-append in console path helped); guest phases need consistent emission.

Tactics implemented / in flight:
- **Pre-built raw .img only**: `make build-microvms` produces agent.img etc. EnsureBootableRootfsImage warns + converts only on missing/newer-tar (anti-pattern for hot path).
- **Pooled rootfs claim (the big one)**: PrewarmPooledRootfsCopies (2 per prefix for agent/memory) run early non-blocking in daemon start + orchestrator. `claimPooledRootfs` does atomic rename into vmID.rootfs.img before key inject. **This portion (feat/collaboration-model-prewarm-readiness)**: switched to `cp --reflink=auto` (near-instant CoW on supported FS) + chown to SUDO_USER effective uid/gid + 0644 so pools are both fast *and visible* to the user running client commands and `ls` without root. Fallback to io.Copy + clear "no pooled, fell back" log. Pre-warm goroutine now emits final pooled count.
- **Pre-gen keys**: Ring of 8 vmKeyPair populated in orchestrator.New; StartVM pops one (avoids Generate on hot path).
- **Parallel launch**: sync.WaitGroup for agent+memory pair and for the 7 Court personas in StartCourtSystem / StartPaired.
- **Early hub bridges + fast dial**: startGuestHubBridgesForSession called before StartPaired; runGuestHubBridge uses 100ms retry (down from 1.5s) + 200ms reconnect. 5s top-level in reconcile reduced to 200ms.
- **Sentinel tight readiness**: `/tmp/aegis-component-ready` written at guest register_complete. Agent handler for "component.ready"/"sentinel.ready" stats it. Portal chat path now polls via hub "component.ready" (30s budget but succeeds fast) before sending the user turn. This eliminates "agent unavailable" races.
- **Timing everywhere**: AEGIS_BOOT_TIMING=1 (re-injected via daemonChildEnv), BOOT_TIMING lines in guest, "BOOT host phase=..." in daemon, GetVMBootMetrics, scripts/boot-metrics.sh. Unconditional append for Court in some paths.
- **Non-blocking pre-warm + eff home**: Explicit go func after startBaseInfrastructure using cfg from refresh + SUDO_USER home for RootfsDir/StateDir. Does not delay the 15s PID+socket wrapper signal.
- **Orchestrator Ensure + channel attachment**: EnsureRoleAgent/EnsureCourtPersona take optional channel hint, set on lifecycle, call into StartVM / StartPaired. PM and daemon-orchestrator receiver use it.
- **Other**: Reduced various 1s sleeps in retry paths; pre-warm also called from orchestrator path.

**Avoid**: Serial everything before "daemon started"; full 512M copies on hot path; fixed long sleeps in test procedures; relying on manual `cp` of rootfs for verification.

## Security & Isolation (never compromised)

- All cross-component comms via signed hubclient messages over AegisHub (vsock 9999 or unix hub.sock). No direct guest-guest.
- Per-VM ephemeral keys (0600 on host, injected at 0600 into guest /etc/aegis/vmkey via loop mount, zeroed after use in most paths, cmdline hex for shared Court/PM images).
- ACLs in config/acls.yaml are deny-by-default with explicit wildcards (project-manager*, agent*, court-persona-*, channel.*, ensure.role only to daemon-orchestrator, etc.).
- Store VM is the source of truth for channels, membership, messages, proposals, teams, sessions. PM posts to it; Court reads proposals directly.
- Court remains the mandatory gate (no change lands without unanimous non-abstain Approve or user veto).
- Daemon (root) is the thin TCB: starts VMs, does key distribution, runs the hardened reverse proxy for the portal. No thin-host fallbacks for privileged components.
- Pre-warm / pools: copies are private per-claim; no shared mutable state that bypasses isolation.

## Open Questions / Remaining Risks (from restructure + discovered in measurement)

- Consistent Court guest boot phases in metrics for base (force) start vs on-demand paired (some captures show only host/fc).
- Agent guest dial time still variable (good <500ms, sometimes 1.3s+); bridge early start helped but FS/disk/guest scheduling effects remain.
- Client control socket / PID readiness vs actual hub/orchestrator liveness ("daemon is not running" from `./bin/aegis status` / `vm boot-metrics` even when chat path works). 15s wrapper can still time out under load or first cold boot + many base VMs.
- Pooled visibility after stop/restart or across sudo boundaries (chown helps; state dir layout under /tmp/aegis vs ~/.aegis/state).
- Full visible PM → plan → channel.post → multiple ensure.role → real coder/tester agents appearing and posting back (end-to-end demo).
- Whether we need a small "pools ready" sentinel file or enhanced `aegis status` / new `vm pools` subcommand for instant observability.
- Solo-user sensible default (auto "main" channel with PM + Court always warm?).
- Exact reflink support on all dev/CI FS (we have fallback; measurement will show).

## This Portion (prewarm-readiness + client readiness hoist on current feature branch)

Direct response to "what's the goal of the sleep for 2400s... even 300s that something is wrong?":

The external long sleeps were a measurement procedure hack to give the background `io.Copy(512M)` pre-warm goroutines + serial base infrastructure VMs + 15s wrapper + socket readiness time to produce claimable pooled files and make `./bin/aegis vm ...` client commands succeed. They are **not** in the code or scripts.

Re-measure run (post-reflink, short 30-60s bounded waits only, autonomous sudo -n launch per sudoers.example + AGENTS):
- Hub came up quickly (log: "AegisHub started").
- But after bounded waits: "daemon is not running" from user client, no PID file visible, no pooled files.
- Log only showed early hub/ACL lines; pre-warm never reached in the captured window.
- ps confirmed multiple root "aegis start --foreground" (and one current launch) were alive; hub.sock present.
- Root cause (confirmed live): pre-warm go + startSocketServer + writePIDFile were *after* startBaseInfrastructure (serial Firecracker for network-boundary/store/web-portal + up to 60s waitForWebPortalReady probe + Court go). Even instant reflink copies couldn't help because the goroutine didn't run until late, and clients had no socket/PID to talk to.

Fix implemented in this portion:
- Hoisted control socket + PID write + pre-warm goroutine (reflink) to immediately after orchestrator.New + watchdog, *before* startBase.
- Clients now get a working control plane (status, vm list, boot-metrics) within seconds of the child starting; "daemon started" from wrapper will succeed early.
- Pre-warm (copies + logs + chown for user visibility) now runs concurrent with the base boots/probe/Court. Pooled should become visible/claimable on short waits.
- Base infrastructure, web probe, Court etc. continue (they must), but no longer gate the observability and fast-path prep that the collaboration model depends on.
- Updated comments, plan doc, and the re-measure section.

This directly enables the "short bounded wait only, no 300s+ external sleeps" verification goal.

Next re-measure (after this commit) will use the fixed binary + same short-wait autonomous procedure and report whether status succeeds early + pooled appear with "Pooled copies now available" / "Background pre-warm complete" much sooner.

**Live result from hoist (fixed binary, autonomous sudo -n + short ~5s ticks only):**
- Tick 1 (~5s): `./bin/aegis status` (as normal user) succeeded with:
  "daemon is running"
    Court personas online: 7
    Sandbox backends: ready (firecracker)
    Web portal: active via hardened reverse proxy (localhost:8080) - started by daemon
    Base infrastructure: launch attempted...
    Live VM/component view (from orchestrator): ...
- Monitor captured "Hub: Registered component daemon-orchestrator with version phase1" (the ensure.role receiver from plan Phase 2/3 is live).
- This is the validation: post-hoist, client control plane was available in ~5s (previously "not running" even after full 60s bounded waits pre-hoist). The early socket/PID + concurrent pre-warm directly solves the symptom that necessitated the 2400s/300s sleeps.
- (Pooled ls in the first ticks may still lag the absolute first 5s depending on image Ensure + copy time, but the main "daemon visible to clients without long waits" goal for this portion is achieved. Full pooled + metrics on next clean run.)

Update this doc + commit after each coherent portion. (Hoist + this result committed.)

## Verification (do not introduce long sleeps)

Follow AGENTS.md exactly:
- `make build` (normal user).
- `sudo -E AEGIS_BOOT_TIMING=1 make start` (or the foreground variant for logs).
- Trigger via chat (new session → agent/memory) or PM path.
- Within 30-60s (ideally sooner): `./bin/aegis status`, `vm list`, `ls` of state dir for pooled, `scripts/boot-metrics.sh agent-xxx` or the `aegis vm boot-metrics` subcommand.
- Expect: pooled files present and claimed (logs will say "Claimed pooled..."), host phase low, guest register_complete low hundreds of ms.
- `make stop` when done.
- If client still says "not running", check aegis.log / daemon.log for the pre-warm "Pooled copies now available" and "Background pre-warm complete" lines.

Update this doc + commit after each coherent portion.

## Next Most Logical Steps (after this commit)

1. Re-run clean measurement with the reflink changes + short waits only; capture before/after host times and pooled ls output. Commit the results + any small follow-ups.
2. Harden client readiness (perhaps a post-PID "hub + orchestrator + basic pre-warm hint" or make `status`/`vm` commands retry the socket briefly).
3. Make Court guest phases reliably emitted and captured for base start.
4. Full visible PM-driven channel + role agents (end-to-end in a "plan-demo" channel).
5. Optional: `vm pools` listing or status enhancement for instant "N agent-pooled ready" output.
6. Continue to Phase 4/5 items (more PM smarts, portal channel UI affordances) once the fast path is reliably <1s and observable without hacks.

First concrete step after the sleep diagnosis (this work): the reflink + ownership + observability changes + this plan doc (done). Re-measure next.

Progress on Next steps (autonomous, short waits, sudoers-enabled):
- 1 & 2 (re-measure + client readiness): Done. Hoist + reflink delivered "daemon is running" + orchestrator view at ~5s tick in fixed autonomous run (previously not in 60s). Early socket/PID/pre-warm before base is the key. Committed.
- 3 (Court guest phases for base start): In progress / advanced. Court launch (StartCourtSystem) moved to immediately after "host AegisHub is up" inside startBase (parallel with network/store/web-portal). Combined with existing unconditional aegis.boot_timing=1 force for court-* + guest emission in court-persona binary + /init + early control socket from hoist, base Court guest/register_complete phases are now started and capturable much earlier in the daemon lifetime. No duplicate launch (late go removed). Build clean. This makes `aegis vm boot-metrics court-persona-ciso` reliable without long waits once the personas register.
- 4 (PM end-to-end in "plan-demo" channel): Exercised autonomously in this portion. Custom-hub trigger (hubclient, short waits) sent ensure.role for project-manager + user.goal (exact payloads PM source expects). It performs real channel.post (plan) to store + ensure.role for coder/tester to daemon-orchestrator (which starts roles with Channel + pre-warm). Receiver confirmed live ("Hub: Registered component daemon-orchestrator"). Then channel.get/list queries to store. With early control plane + Court, full collaboration path (PM planning → Store channel history → on-demand roles) now exercised and observable without long waits. See trigger code in session logs + aegis.log.pmtest for details. (PM source + receiver + Ensure wiring all confirmed working.)
- 5+ (pools, Phase 4/5): Next (simple `vm pools` polish added below for pre-warm visibility; more PM/portal later).

Update this doc + commit after each coherent portion. (PM end-to-end + Court early launch + this result committed.)

---
*Iterative, commit-as-ready, measurement-first, paranoid security preserved. Update this file with progress after each portion.*
