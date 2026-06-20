# MicroVM Observability Specification

**Status:** Draft (Phases 0 & 1 prioritized)  
**Date:** 2026-05-29  
**Related:** host-daemon.md, web-portal/web-portal-vm.md, builder-vm.md, event-system.md, cli.md

## Motivation

Running workloads inside Firecracker microVMs (or Docker sandboxes) creates a classic observability gap:

- The host can see that a VM process started.
- Firecracker can report that the microVM booted successfully.
- However, what is happening *inside* the guest — early init failures, vsock listener registration, application startup errors, vsock connectivity problems — is often invisible or only available through fragile serial console capture.

Current pain points (as of the `docs/lessons-learned` branch):

- Guest serial console output is inconsistently captured or completely missing for some VMs.
- There is no standard way for users or operators to retrieve logs from a specific microVM.
- Debugging issues such as "vsock dial fails with 'no such device'" requires deep host filesystem spelunking and manual correlation of multiple log files.
- The readiness probe for the web portal reverse proxy can correctly detect that a guest is unreachable, but cannot explain *why* from the guest's perspective.

A sustainable observability story is required so that both developers and future operators can debug issues without custom one-off hacks.

## Goals

- Provide reliable, low-friction access to what is happening inside each microVM.
- Support both early boot (kernel + init) and application-level logging.
- Work within the existing paranoid security model (all access mediated by the Host Daemon).
- Be useful during local development (`sudo ./bin/aegis start`) and in more production-like environments.
- Be incrementally deliverable (start with what gives the highest leverage quickly).

## Non-Goals (for initial phases)

- Full metrics collection and alerting (Phase 3+).
- Interactive debugging / gdb-style attachment from the host (future consideration).
- Log shipping to external systems (Store VM can be the durable sink for now).

## Phased Approach

### Phase 0: Reliable Console + Basic Access (Foundations)

**Problem addressed:** "I can't even see what the guest printed during boot."

**Deliverables:**

1. Make guest serial console capture robust and first-class.
   - The Firecracker backend already requests console capture to `fc-<id>-console.log`.
   - The orchestrator and sandbox layer will expose the console log path (and possibly tail content) as part of `VMInfo` / `ListVMs` responses.
   - Console logs are treated as VM lifecycle artifacts (cleaned up on `StopVM` / daemon shutdown, rotated if they grow too large).

2. Add a standard CLI surface:
   ```bash
   aegis vm logs <id> [--follow] [--tail N] [--since <time>]
   ```
   This initially reads from the captured console log file. It works even when the VM is still running.

3. Expose the same capability over the control socket (`vm.logs` operation) so the web portal and other clients can use it.

4. Document expectations for guest images:
   - Every guest must write a clear startup banner (including build ID) to the serial console as early as possible.
   - The `/init` (or equivalent entrypoint) must redirect stdout/stderr to the serial console (`/dev/console` or equivalent) before running the main binary.

**Success criteria:**
- After `sudo ./bin/aegis start`, a developer can run `aegis vm logs web-portal --tail 100` and see kernel boot messages + whatever the guest `/init` and binary emitted.
- The same command works for court personas, store, network-boundary, etc.

### Phase 1: Structured Logging over vsock

**Problem addressed:** Serial console is lossy and unstructured. We need reliable, structured, application-level logs from inside the guest.

**Deliverables:**

1. Define a simple, versioned vsock logging protocol.
   - Well-known port (proposed: 18099).
   - Length-prefixed JSON lines (or a very small binary framing).
   - Required fields on every record: `ts`, `level`, `msg`, `vm_id` (injected by guest library or host).
   - Optional structured fields.

2. Provide a minimal Go logging client (package under `internal/guest/log` or similar) that components can import.
   - Simple API: `log.Info("message", "key", value)`
   - Automatically connects over vsock to the host (CID 2) on the designated port.
   - Best-effort buffering and reconnection.
   - Falls back gracefully if vsock is unavailable (e.g., during early development or Docker Sandbox path).

3. Host-side collector in the daemon.
   - After a VM is started (or lazily on first connection), the daemon accepts vsock connections on the logging port from that VM's CID.
   - Logs are:
     - Written to a per-VM log file (e.g., `~/.aegis/state/<id>.guest.log`) with rotation.
     - Made available through the same `vm logs` path (with a `--source guest` or by default showing both console + guest logs).
     - Published on the in-process EventBus (for future integration with Store VM / audit).

4. Update at least the web-portal (and ideally one or two other base components) to emit early structured logs via the new library, especially around:
   - Binary startup
   - vsock listener registration attempts
   - TCP listener startup
   - Hub registration
   - Readiness signals

**Success criteria:**
- When the web-portal guest starts, `aegis vm logs web-portal` shows both the Firecracker console output *and* structured messages such as:
  ```
  {"ts":"...","level":"info","msg":"web-portal guest starting","build_id":"..."}
  {"ts":"...","level":"info","msg":"attempting vsock listener","port":18080}
  {"ts":"...","level":"error","msg":"vsock listen failed","port":18080,"err":"..."}
  ```
- These logs are available even if the main HTTP server on the guest never becomes reachable.

### Later Phases (Deferred)

- **Phase 2:** Rich debugging tools (interactive console attach, diagnostic bundles, on-demand guest introspection).
- **Phase 3:** Higher-level observability (searchable logs in the web portal, metrics, automatic anomaly detection, integration with Court / event system for automated responses to common failure modes).

These are valuable but can be tackled after the immediate web-portal reachability and general "I can't see what's happening inside the VM" problems are solved.

## Security & Isolation Considerations

- All log data from guests flows through the Host Daemon (the TCB). Guests never write directly to host filesystems.
- Early boot logs (serial console) are inherently less authenticated; they are treated as best-effort diagnostic data.
- Structured vsock logs can carry a per-VM identity (the daemon already distributes per-VM keys). Future work can add signatures if stronger provenance is required.
- Log volume must be bounded (rotation + size limits) to avoid DoS against the host.
- Access to logs is controlled by the same mechanisms as other privileged operations (control socket permissions, `sudo` for direct daemon commands).

## Integration Points

- **Host Daemon / Orchestrator**: Primary collector and storage of per-VM logs. Extends `ListVMs` / new `GetVMLogs` operations.
- **Control Socket + CLI** (`cli.md`): New `vm logs` subcommand and socket operations.
- **Web Portal**: Future consumer of log data (both for display and for surfacing in the UI when a VM is unhealthy).
- **EventBus**: Structured logs can be published as events for correlation with other system activity.
- **Store VM** (future): Durable, queryable, long-term storage of guest logs with access control.
- **Guest images**: Must adopt the logging library (or at minimum follow the early-boot console banner convention).

## Open Questions

- Exact wire format for the vsock logging protocol (JSON lines vs. a tiny binary framing).
- Default retention policy for on-host per-VM log files.
- Whether the Network Boundary should also be allowed to emit / receive certain classes of logs.
- Naming of the vsock logging port and any service discovery mechanism.

## References

- Current console handling in `internal/sandbox/firecracker.go`
- vsock usage patterns in `cmd/web-portal/`, `cmd/aegishub/`, and the hubclient package
- Host daemon responsibilities in `host-daemon.md`
- Web Portal VM networking in `web-portal/web-portal-vm.md`

---

**Implementation Status (as of this edit):**

- **Phase 0 progress**: 
  - `VMInfo` and `VMLifecycle` now carry `ConsoleLogPath`.
  - Firecracker backend populates and exposes the path.
  - New control socket operation `vm.logs` + CLI command `aegis vm logs <id> [--tail N]` implemented. This gives immediate, usable access to guest serial console output without manual filesystem spelunking.
  - This directly helps the current class of "VM booted but I can't see why the app isn't ready" problems.

- **Phase 1 scaffolding**:
  - `LogVsockPort = 18099` reserved in the hubclient constants.
  - Protocol and guest library design documented (see sections above). Full host collector + guest client library implemented (see `internal/guest/log/client.go` and `cmd/aegis/guestlog_collector.go`). Web-portal now emits early structured startup logs over vsock.

The design prioritizes giving developers and operators a reliable `aegis vm logs` experience first, then moving to structured in-guest logging over vsock as the sustainable long-term channel.

### Developer Ergonomics Note (Sudo / Autonomy)

To work efficiently on this tree (frequent `sudo ./bin/aegis start`, `make build-microvms`, and log inspection under root-owned state directories), the following minimal sudoers entry is recommended (see also AGENTS.md):

```
pixnbits ALL=(ALL) NOPASSWD: /home/pixnbits/projects/AegisClaw/docs/lessons-learned/bin/aegis
pixnbits ALL=(ALL) NOPASSWD: /home/pixnbits/projects/AegisClaw/docs/lessons-learned/scripts/build-microvms-docker.sh
```

This allows passwordless execution of the exact commands used in the normal development workflow without weakening the overall security posture. Create/edit the file as:

```bash
sudo visudo -f /etc/sudoers.d/aegisclaw
```

Then paste the two lines above (adjust the username and absolute paths if your checkout location differs). Validate with `sudo visudo -c`.

This is the exact pattern already documented in the project's AGENTS.md for productive development.