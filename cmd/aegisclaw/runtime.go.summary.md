# runtime.go — cmd/aegisclaw

## Purpose
Initialises and manages all runtime singletons used by daemon commands. Provides the `runtimeEnv` aggregate struct and the `initRuntime()` factory function. All subsystem stores are initialised lazily via `sync.Once` so successive CLI invocations in the same process share state.

## Key Types / Functions
- `runtimeEnv` — holds logger, config, kernel, Firecracker runtime, skill registry, proposal/composition/memory/event-bus/worker/lookup/git stores, LLM proxy, vault, egress proxy, workspace content, sessions store, task executor, and IDs for the agent, hub, and portal VMs.
- `initRuntime()` — loads config, creates logger, initialises `kernel.Kernel`, and (once) constructs all singletons via `runtimeOnce`.
- `resetRuntimeSingletons()` — zeroes all singletons for integration test isolation.
- `loadWorkspace()` — loads workspace prompt files from `cfg.Workspace.Dir`; returns an empty `Content` on error.
- `loadOrCreateMemoryIdentity()` — loads or generates the age X25519 identity for the memory store.
- `generateVMID(prefix)` — returns a `<prefix>-<8-hex>` VM ID used for all VMs started by the daemon.

## System Fit
Every daemon-facing command (start, status, chat, audit, skill, etc.) calls `initRuntime()` to obtain a fully wired environment. Integration tests call `resetRuntimeSingletons()` between scenarios.

## Notable Dependencies
- `filippo.io/age` — memory store identity
- `github.com/google/uuid` — VM ID generation
- `go.uber.org/zap` — structured logging
- All major internal packages: `kernel`, `sandbox`, `proposal`, `composition`, `memory`, `eventbus`, `worker`, `vault`, `lookup`, `llm`, `sessions`, `workspace`, `runtime/exec`
