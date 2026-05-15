# 08 - Daemon TCB Extraction

**Goal**: Extract remaining business logic (court engine, build orchestrator, dashboard, event dispatcher, proposal reconciliation, etc.) out of the Host Daemon into appropriate components (AegisHub, dedicated sandboxes, or internal services).

## Why This Matters
After steps 02–05, the daemon should be minimal. This step finishes the job by moving everything that isn't pure TCB responsibility.

## Tasks

1. **Identify all business logic still in daemon**
   - Court initialization and review handlers
   - BuildOrchestrator and pipeline
   - Dashboard server
   - Event dispatcher and proposal reconciliation
   - Git/PR/workspace handlers (move to AegisHub or dedicated VMs)

2. **Migrate logic**
   - Move court-related code to AegisHub or a Court Scribe VM
   - Move build orchestration to a dedicated Builder service or AegisHub
   - Move dashboard to its own lightweight process or Web Portal VM

3. **Update IPC / API surface**
   - Keep only minimal control plane handlers in daemon
   - All complex operations routed through AegisHub

4. **Tests**
   - Verify daemon LOC and memory targets still met
   - Ensure no regression in functionality

## Acceptance Criteria
- Daemon contains **only** TCB responsibilities (sandbox lifecycle, socket, signing, watchdog, key distribution)
- All business logic moved to appropriate components
- Full functionality preserved

**Dependencies**: Follows 02–05 and 07
**Estimated effort**: 2–3 days.

**Owner**: TBD
**Status**: Ready after 07