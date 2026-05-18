# Phase 3: Handler Extraction Migration Checklist

**Goal**: Systematically move control-plane handlers out of the Host Daemon and into AegisHub via thin proxies.

**Current State**: Real `AegisHubClient` is wired by default. Chat handlers are largely proxied.

---

## General Extraction Pattern (for every handler)

1. Create a `makeXxxProxy(env *runtimeEnv) api.Handler` function.
2. Inside the proxy, call `env.AegisHubClient.ForwardXxx(...)` (add method to interface if needed).
3. Update registration in `registerExtendedDaemonAPI` (or equivalent).
4. Add deprecation comment on the old direct handler.
5. Test that the proxy path is taken.
6. Mark as done in this checklist.

---

## Priority 1: EventBus & Coordination (High Value)

| Handler                        | File                        | Current State          | Action                              | Status     | Notes |
|--------------------------------|-----------------------------|------------------------|-------------------------------------|------------|-------|
| `makeApprovalsListHandler`     | `eventbus_handlers.go`      | Direct `env.EventBus`  | Create proxy + add method to client | To Do      |       |
| `makeApprovalsDecideHandler`   | `eventbus_handlers.go`      | Direct `env.EventBus`  | Create proxy + add method to client | To Do      |       |
| `makeTimersListHandler`        | `eventbus_handlers.go`      | Direct `env.EventBus`  | Create proxy                        | To Do      |       |
| `makeSignalsListHandler`       | `eventbus_handlers.go`      | Direct `env.EventBus`  | Create proxy                        | To Do      |       |

**Next Step**: Start with Approvals handlers.

---

## Priority 2: Sessions

| Handler                  | File                           | Current State             | Action                        | Status | Notes |
|--------------------------|--------------------------------|---------------------------|-------------------------------|--------|-------|
| `sessions.list`          | `daemon_handlers_extended.go`  | Partially proxied         | Complete proxy                | To Do  |       |
| `sessions.history`       | `daemon_handlers_extended.go`  | Stubbed                   | Create proxy                  | To Do  |       |
| `sessions.send`          | `daemon_handlers_extended.go`  | Stubbed                   | Create proxy                  | To Do  |       |
| `sessions.spawn`         | `daemon_handlers_extended.go`  | Stubbed                   | Create proxy                  | To Do  |       |

---

## Priority 3: Workers

| Handler             | File                           | Current State                  | Action                              | Status | Notes |
|---------------------|--------------------------------|--------------------------------|-------------------------------------|--------|-------|
| `worker.list`       | `daemon_handlers_extended.go`  | Some direct access             | Create proxy                        | To Do  |       |
| `worker.status`     | `daemon_handlers_extended.go`  | Some direct access             | Create proxy                        | To Do  |       |
| `spawn_worker` tool | `tool_registry.go`             | Still registers local handler  | Move registration or proxy          | To Do  |       |

---

## Priority 4: Skills & Tasks

| Area             | Status          | Notes                                      | Next Action                  |
|------------------|-----------------|--------------------------------------------|------------------------------|
| Skill handlers   | Mostly stubbed  | Some legacy registry access remains        | Clean up remaining direct paths |
| Task handlers    | Mostly stubbed  | Low priority                               | Monitor for direct access    |

---

## Priority 5: Cleanup & Hardening

- [ ] Remove any remaining direct `env.EventBus`, `env.WorkerStore`, or `env.Registry` usage outside of core TCB.
- [ ] Add more methods to `AegisHubClient` interface as needed.
- [ ] Update `docs/planning/03-tcb-boundaries.md` with extraction progress.
- [ ] Verify `go build` succeeds after each batch.

---

## AegisHubClient Interface Extensions Needed

Add these methods as we extract handlers:

- `ForwardApprovalsList(ctx, data)`
- `ForwardApprovalsDecide(ctx, data)`
- `ForwardTimersList(ctx, data)`
- `ForwardSessionsList(ctx, data)`
- `ForwardWorkerList(ctx, data)`

---

**Let's iterate through this checklist one section at a time.**
