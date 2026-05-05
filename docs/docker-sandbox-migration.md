# Docker Sandbox Migration Design

**Status**: Proposal  
**Version**: 1.0  
**Date**: May 2026  
**Author**: Engineering (AegisClaw)  
**Relates to**: `docs/architecture-addendum.md` (Phase 2–3), `internal/sandbox/orchestrator.go`

---

## 1. Background and Motivation

AegisClaw currently runs every workload — skills, Governance Court reviewers, AegisHub, the builder pipeline — inside **Firecracker microVMs** managed by `FirecrackerRuntime` (`internal/sandbox/firecracker.go`). This provides kernel-level hardware virtualisation and is the strongest possible isolation boundary.

Docker Sandboxes has now landed on Linux, bringing:

- **Sub-second cold start** (vs. ~300 ms for Firecracker but with less guest-boot overhead).
- **Familiar OCI image toolchain** — rootfs images are already built with Docker (`Dockerfile.rootfs`; `scripts/build-microvms-docker.sh`). The ext4 conversion step disappears.
- **Lower host-side requirements** — no `/dev/kvm`, no jailer UID-mapping, no kernel image asset to provision (`provision.EnsureAssets`).
- **Better developer ergonomics** — `docker exec`, live logs, `docker diff`, image layer caching.

The `Orchestrator` interface (`internal/sandbox/orchestrator.go`) was designed explicitly to allow alternative backends. The `IsolationMode` type and `NewOrchestrator` factory are the natural extension points. No callers outside `orchestrator.go` depend on `FirecrackerRuntime` directly.

The migration keeps AegisClaw's paranoid-by-design guarantees: default-deny networks, capability dropping, read-only rootfs, secret injection via controlled IPC, and full audit logging through the kernel.

---

## 2. Current Architecture (Firecracker)

```
┌─────────────────────────────────────────────────────┐
│  Host (root daemon)                                  │
│  ┌──────────┐   LaunchSandbox()   ┌───────────────┐ │
│  │Orchestrat│ ──────────────────▶ │FirecrackerRunt│ │
│  │or        │                     │ime            │ │
│  └──────────┘                     └───────┬───────┘ │
│         ▲  SendToSandbox()                │         │
│         │  (vsock AF_VSOCK)        firecracker +    │
│         │                          jailer process   │
│  ┌──────┴──────┐   nftables         ┌────┴────────┐ │
│  │EgressProxy  │ ◀── TAP device ──▶ │microVM      │ │
│  │(vsock:1026) │                    │guest-agent  │ │
│  └─────────────┘                    │vsock:1024   │ │
│                                     └─────────────┘ │
└─────────────────────────────────────────────────────┘
```

Key components and their Firecracker-specific coupling:

| Component | Firecracker coupling | File |
|---|---|---|
| `FirecrackerRuntime` | Creates ext4 rootfs copy, manages `firecracker.Machine` | `firecracker.go` |
| `firecrackerOrchestrator` | Wraps runtime; implements `Orchestrator` | `orchestrator.go` |
| Network isolation | TAP device + `/30` subnet + `nftables` via `netpolicy.go` | `firecracker.go`, `netpolicy.go` |
| IPC (host→guest) | AF_VSOCK CID allocation, `vsock.sock` UDS | `firecracker.go`, `start.go` |
| Egress proxy | Listens on `vsock.sock_1026`, splices TLS | `internal/llm/egressproxy.go` |
| Snapshots | Firecracker pause/resume + VM state file | `snapshot.go` |
| Rootfs build | `Dockerfile.rootfs` → ext4 via `build-microvms-docker.sh` | `scripts/` |
| Asset provisioning | Downloads vmlinux + rootfs on first run | `internal/provision/` |

---

## 3. Target Architecture (Docker)

```
┌──────────────────────────────────────────────────────────────┐
│  Host daemon (non-root possible)                              │
│  ┌──────────┐   LaunchSandbox()    ┌────────────────────────┐│
│  │Orchestrat│ ───────────────────▶ │DockerRuntime           ││
│  │or        │                      │(docker SDK / CLI)      ││
│  └──────────┘                      └────────────┬───────────┘│
│         ▲  SendToSandbox()                       │            │
│         │  (Unix socket bind-mount)       docker run         │
│  ┌──────┴──────┐   Docker network    ┌────┴─────────────────┐│
│  │EgressProxy  │ ◀── user-defined ──▶│container             ││
│  │(TCP/unix)   │    bridge + iptables│guest-agent           ││
│  └─────────────┘                    │/run/aegis/agent.sock  ││
│                                     └──────────────────────┘ │
└──────────────────────────────────────────────────────────────┘
```

### 3.1 New `IsolationMode` and `DockerRuntime`

Add `IsolationDocker IsolationMode = "docker"` to `orchestrator.go`.  
Implement `DockerRuntime` in a new file `internal/sandbox/docker.go` that satisfies `SandboxManager` and the extended internal interface (including `VsockPath`-equivalent and `SendToVM`-equivalent).

`NewOrchestrator` gains a `docker` branch:

```go
case IsolationDocker:
    if dr == nil {
        return nil, fmt.Errorf("orchestrator: DockerRuntime is required for isolation_mode=%q", IsolationDocker)
    }
    return &dockerOrchestrator{rt: dr}, nil
```

### 3.2 Container Spec Mapping

`SandboxSpec` is intentionally backend-agnostic. The Docker runtime interprets each field as follows:

| `SandboxSpec` field | Docker mapping |
|---|---|
| `ID` | Container name / label `aegisclaw.sandbox.id` |
| `Resources.VCPUs` | `--cpus` |
| `Resources.MemoryMB` | `--memory` |
| `NetworkPolicy.NoNetwork` | `--network none` |
| `NetworkPolicy.AllowedHosts` + `EgressMode=proxy` | Attach to per-sandbox bridge; egress proxy enforces FQDNs |
| `NetworkPolicy.AllowedHosts` + `EgressMode=direct` | `--network` + `iptables` OUTPUT rules (mirrors current nftables path) |
| `RootfsPath` | Docker image tag or `--rootfs` OCI bundle path |
| `WorkspaceMB` | Bind-mount of a `tmpfs` or host directory at `/workspace` |
| `SecretsRefs` | Resolved from vault; written into a host-side `tmpfs` volume mounted at `/run/secrets` |
| `VsockCID` | **Unused** — replaced by Unix socket path (see §3.3) |
| `KernelPath` / `InitPath` | **Unused** — Docker manages the kernel |

`SandboxSpec.VsockCID`, `KernelPath`, and `InitPath` are left in the struct for backward compatibility; `DockerRuntime.Create` validates that they are zero/empty and ignores them.

### 3.3 IPC: Replacing vsock with a Unix Socket

The guest-agent currently listens on `AF_VSOCK` port 1024. For Docker the host communicates over a Unix domain socket bind-mounted into the container.

**Host side**: `DockerRuntime` creates a per-sandbox directory
`$StateDir/<sandboxID>/` and passes `/run/aegis/host.sock` as a bind-mount into the container at `/run/aegis/agent.sock`.

**Guest agent** (`cmd/guest-agent/main.go`): Add a build tag or runtime flag `--transport=unix` (default: `vsock` for backward compat). When `unix`, listen on `AF_UNIX /run/aegis/agent.sock` instead of vsock.  The `Request`/`Response` JSON protocol is unchanged.

**`SendToSandbox`** in `dockerOrchestrator` connects to
`$StateDir/<id>/host.sock` (the host end of the bind-mount) rather than dialling a vsock CID.

This means secrets injection (`doInjectSecrets`), LLM proxy (`AegisHub`), and all other host→guest calls work without changes to their JSON protocol.

### 3.4 Network Isolation

Replace TAP device + nftables with a **per-sandbox Docker bridge network**:

- `docker network create --driver bridge --internal aegis-<sandboxID>` for no-egress sandboxes.
- For sandboxes with `EgressMode=proxy`: create an internal bridge plus attach the container to a shared `aegis-egress` network that only routes to the egress proxy container.
- For `EgressMode=direct`: apply `iptables OUTPUT` rules keyed on the container's veth MAC/IP (same logic as the current nftables `PolicyEngine`, but using iptables rather than nft tables — or nftables with `--netdev` hooks keyed on the veth interface).

The `netpolicy.go` `PolicyEngine` abstraction is reusable; only the low-level rule applier changes.

Teardown: `docker network rm aegis-<sandboxID>` on `Stop`/`Delete`.

### 3.5 Egress Proxy

`EgressProxy.StartForVM` currently receives a vsock UDS base path. For Docker, pass the Unix socket path of the per-sandbox bridge gateway instead. Internally the proxy still speaks the same CONNECT-tunnel protocol; only the transport changes from vsock splice to TCP/unix on the bridge interface.

Alternatively, run the egress proxy as a sidecar container on the `aegis-egress` bridge network. This is architecturally cleaner but deferred to Phase 2.

### 3.6 Rootfs: OCI Images Instead of ext4

The `Dockerfile.rootfs` multi-stage build already produces well-defined filesystem layers for `guest`, `aegishub`, `portal`, and `builder`. For Docker sandboxes these are used directly as OCI images — no ext4 conversion needed.

```
scripts/build-microvms-docker.sh  →  docker build → push to local registry
                                      OR save as OCI tarball
```

`SandboxSpec.RootfsPath` carries either:
- An image reference (`aegisclaw/guest:latest`) for Docker mode, or
- An absolute ext4 path (`/var/lib/aegisclaw/rootfs-templates/guest.ext4`) for Firecracker mode.

`DockerRuntime.Create` detects which form it has received and validates accordingly.

Asset provisioning (`provision.EnsureAssets`) gains a `--mode=docker` path that pulls/tags the image instead of downloading a kernel + ext4 blob.

### 3.7 Sandbox Startup

**Cold start sequence** (Docker):
1. `docker pull <image>` (skipped if already present — images are pre-warmed at daemon start).
2. `docker create --name aegis-<id> --network aegis-<id> --cap-drop ALL --cap-add <declared> --security-opt seccomp=<profile> --security-opt apparmor=<profile> --read-only --tmpfs /run:size=64m --tmpfs /tmp:size=32m -v $StateDir/<id>/host.sock:/run/aegis/agent.sock <image>`.
3. `docker start aegis-<id>`.
4. Audit log: `kernel.SignAndLog(ActionSandboxStart, ...)`.
5. Poll `/run/aegis/agent.sock` with exponential backoff (same retry logic as vsock boot-wait in `SendToVM`).

Expected cold-start time: **< 100 ms** (no kernel boot overhead).

**Idle/warm standby**: When a skill has been idle for `idle_timeout` (default: 5 min, configurable), `docker pause aegis-<id>` freezes the cgroup. Resume on next `SendToSandbox` via `docker unpause` before forwarding the request. Pause/unpause latency is typically < 10 ms.

**On-demand restart**: If the container exits unexpectedly, the orchestrator detects this during `SandboxStatus` (container state = `exited`), re-runs `docker start`, and re-injects secrets. The registry reactivation path in `start.go` already handles the re-launch case.

### 3.8 Sandbox Shutdown

**Graceful stop**:
1. Send `{"type":"shutdown"}` request via Unix socket → guest agent flushes state and exits.
2. `docker stop --time=5 aegis-<id>` (SIGTERM → 5 s → SIGKILL).
3. `docker network rm aegis-<id>`.
4. Remove state directory `$StateDir/<id>/`.
5. Audit log: `ActionSandboxStop`.

**Forced stop** (cleanup / daemon shutdown):  
`docker kill aegis-<id>` then `docker rm -f aegis-<id>`.  
`Cleanup()` iterates all running sandboxes — identical to the Firecracker path in `cleanup.go`.

**Idle expiry**:  
A background goroutine (already exists as part of the event bus timer infrastructure) fires every minute, calls `SandboxStatus` on each registered skill, and issues `docker pause` for sandboxes that have been idle longer than `idle_timeout`. An idle sandbox in `paused` state is distinguished from `running` in `SandboxInfo.State` via a new `StatePaused SandboxState = "paused"`.

### 3.9 Snapshots (CRIU Checkpointing)

Firecracker's pause/snapshot/restore maps to Docker's experimental **CRIU checkpoint**:

```
docker checkpoint create aegis-<id> <label>
docker start --checkpoint <label> aegis-<id>   # restore
```

`snapshot.go` is refactored into a `Snapshotter` interface with `FirecrackerSnapshotter` and `DockerSnapshotter` implementations. `SnapshotMeta` gains a `Backend` field (`"firecracker"` | `"docker"`).

Docker CRIU checkpoints require `--security-opt seccomp=unconfined` (or a custom profile that allows CRIU syscalls) and the `criu` binary on the host. This is gated behind a `checkpoints.enabled` config flag, defaulting to `false`.

### 3.10 Security Profile

Equivalent security controls translated to Docker primitives:

| Firecracker control | Docker equivalent |
|---|---|
| Hardware VM boundary | Seccomp filter + AppArmor/SELinux profile |
| Jailer UID isolation | User namespace remapping (`--userns-remap`) |
| Read-only rootfs | `--read-only` + `--tmpfs /run,/tmp` |
| Capability dropping | `--cap-drop ALL --cap-add <declared>` |
| No network by default | `--network none` |
| nftables per-sandbox | Per-sandbox bridge + iptables OUTPUT rules |
| vsock-only host access | Unix socket bind-mount (no host network namespace) |

A default seccomp profile (`config/seccomp-sandbox.json`) is added, derived from Docker's default profile with additional syscall restrictions (no `ptrace`, no `mount`, no `kexec_load`).

### 3.11 Dual-Mode Transition

During the transition period both backends coexist:

- `aegisclaw.toml`: `[sandbox] isolation_mode = "docker"` (new default on supported hosts) or `"firecracker"` (explicit opt-in for maximum isolation).
- `NewOrchestrator` accepts both modes; callers are unchanged.
- `aegisclaw doctor` detects Docker availability and suggests the appropriate mode.
- Deprecation warning logged at startup when `isolation_mode = "firecracker"` (post-transition).

### 3.12 Files Changed

| File | Change |
|---|---|
| `internal/sandbox/orchestrator.go` | Add `IsolationDocker`, `dockerOrchestrator`, update `NewOrchestrator` |
| `internal/sandbox/docker.go` | **New** — `DockerRuntime`, `dockerOrchestrator` implementation |
| `internal/sandbox/spec.go` | Add `StatePaused`; add `DockerImage` field to `SandboxSpec`; document `VsockCID` as Firecracker-only |
| `internal/sandbox/snapshot.go` | Extract `Snapshotter` interface; add `DockerSnapshotter` |
| `internal/sandbox/netpolicy.go` | Extend `PolicyEngine` for iptables backend |
| `internal/sandbox/cleanup.go` | Handle `StatePaused` and `docker rm -f` path |
| `cmd/guest-agent/main.go` | Add `--transport` flag; support `AF_UNIX` listener |
| `internal/llm/egressproxy.go` | Accept `net.Conn` factory instead of vsock path |
| `cmd/aegisclaw/start.go` | Remove `provision.EnsureAssets` Firecracker-specific paths; add Docker pre-warm |
| `internal/config/config.go` | Add `Sandbox.IsolationMode`, `Sandbox.IdleTimeout`, `Sandbox.Checkpoints` |
| `scripts/build-microvms-docker.sh` | Add `--mode=image` to push OCI images instead of ext4 |
| `Dockerfile.rootfs` | Add `CMD` / `ENTRYPOINT` stubs for direct `docker run` |
| `config/seccomp-sandbox.json` | **New** — default seccomp profile for Docker sandboxes |
| `docs/docker-sandbox-migration.md` | This document |

---

## 4. Phased Rollout

### Phase 1 — Infrastructure (no behaviour change)
- Add `IsolationDocker` constant and stub in `orchestrator.go`.
- Implement `DockerRuntime.Create`/`Start`/`Stop`/`Delete`/`List`/`Status`.
- Add `--transport=unix` to guest-agent.
- Add `StatePaused` and idle-pause goroutine.
- Wire `NewOrchestrator("docker", ...)` behind a feature flag.
- Ensure existing Firecracker tests still pass.

### Phase 2 — Network and Egress
- Per-sandbox bridge networks.
- Extend `PolicyEngine` for iptables Docker backend.
- Refactor egress proxy transport.
- Integration tests for network policy enforcement.

### Phase 3 — Full Parity and Default Switch
- Docker CRIU snapshots (behind `checkpoints.enabled`).
- `provision.EnsureAssets` Docker path.
- Switch default `isolation_mode` to `"docker"`.
- Firecracker becomes `isolation_mode = "firecracker"` explicit opt-in.
- Deprecation notice for Firecracker in `aegisclaw doctor`.

---

## 5. Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Docker seccomp/AppArmor weaker than hardware VM | Validated seccomp profile; AppArmor mandatory mode; user namespace remapping; defence-in-depth unchanged |
| CRIU checkpoint instability | Default off; gated behind `checkpoints.enabled`; fallback to cold restart |
| Idle-pause breaks long-lived skills | Skills declare `keep_alive: true` in proposal to opt out of idle pause |
| Docker daemon as new trusted component | Pin Docker daemon socket access to the AegisClaw daemon process only; no skill can reach `/var/run/docker.sock` |
| Root requirement changes | `--userns-remap` allows non-root daemon; rootless Docker mode tested as secondary target |
| Dual-mode complexity | `Orchestrator` interface keeps divergence in one file; integration tests cover both backends |

---

## 6. Open Questions

1. **Rootless Docker**: Should we target rootless Docker mode from day one (avoids requiring root) or treat it as Phase 4?
2. **AppArmor vs. SELinux**: Which profile to ship by default? Ship both and auto-detect.
3. **Idle timeout default**: 5 minutes pauses Court reviewers mid-conversation. Should per-skill-type defaults differ (30 s for ephemeral workers, 10 min for interactive skills)?
4. **AegisHub as a Docker container**: AegisHub is currently the first microVM launched. Running it as a Docker container changes its IPC address. Design for AegisHub migration is a sub-task.

---

## 7. GitHub Issue Summary

> **Copy the block below verbatim as the body of a new GitHub Issue.**

---

```markdown
## Migrate AegisClaw Sandboxes from Firecracker microVMs to Docker

**Labels**: `enhancement`, `sandbox`, `infrastructure`  
**Milestone**: v0.4 (Docker backend parity)

### Summary

Docker Sandboxes is now available on Linux. This issue tracks the full migration of
AegisClaw's isolation backend from Firecracker microVMs to Docker containers, as
designed in [`docs/docker-sandbox-migration.md`](docs/docker-sandbox-migration.md).

### Motivation

- Sub-100 ms cold starts (no kernel boot overhead).
- OCI images replace ext4 rootfs — the `Dockerfile.rootfs` build already exists.
- No `/dev/kvm` or jailer requirement lowers the install barrier.
- Familiar `docker exec`/`docker logs` developer ergonomics.

The `Orchestrator` interface in `internal/sandbox/orchestrator.go` is the designed
extension point. No callers need to change.

### Work Items

- [ ] **Phase 1 — Core runtime**
  - [ ] Add `IsolationDocker` to `orchestrator.go`
  - [ ] Implement `DockerRuntime` in `internal/sandbox/docker.go`
  - [ ] Add `--transport=unix` to guest-agent; Unix socket IPC replaces vsock
  - [ ] Add `StatePaused` + idle-pause goroutine
  - [ ] Firecracker tests continue to pass (dual-mode)

- [ ] **Phase 2 — Network and egress**
  - [ ] Per-sandbox Docker bridge networks
  - [ ] Extend `PolicyEngine` for iptables Docker backend
  - [ ] Refactor `EgressProxy` transport (vsock → TCP/unix on bridge)
  - [ ] Network policy integration tests

- [ ] **Phase 3 — Full parity + default switch**
  - [ ] Docker CRIU snapshots (behind `checkpoints.enabled`)
  - [ ] `provision.EnsureAssets` Docker pull path
  - [ ] Switch default `isolation_mode` to `"docker"`
  - [ ] Deprecation notice for Firecracker mode in `aegisclaw doctor`

### Security invariants that must be preserved

- Default-deny network policy (enforced via Docker bridge + iptables)
- Read-only rootfs (`--read-only`)
- `--cap-drop ALL` + explicit capability declarations from proposals
- Secrets injected via controlled IPC only (Unix socket, not env vars)
- All sandbox operations signed and audit-logged through the kernel

### Design document

Full design, file-change table, risk register, and open questions:
[`docs/docker-sandbox-migration.md`](docs/docker-sandbox-migration.md)

### Definition of Done

- `aegisclaw start` launches all sandboxes via Docker when `isolation_mode = "docker"`.
- All existing integration tests pass against both backends.
- `aegisclaw doctor` correctly identifies missing Docker or Firecracker and guides setup.
- Security review (Governance Court + external) signs off on seccomp + AppArmor profiles.
```
