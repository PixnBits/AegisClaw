# start.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw start` command — the daemon boot sequence. Provisions Firecracker assets, launches the AegisHub system VM first (hard boot requirement), initialises the IPC bridge, starts the API server, and registers all HTTP handlers.

## Key Types / Functions
- `runStart(cmd, args)` — main entry point; owns the ordered startup sequence.
- Startup order:
  1. Load/init runtime (`initRuntime`).
  2. Provision Firecracker kernel + rootfs images.
  3. Launch AegisHub VM; wait for it to register on the IPC bus.
  4. Start IPC bridge (vsock ↔ message-hub).
  5. Build `api.Server`, register all route handlers.
  6. Start optional daemons: Gateway, Dashboard/Portal, EventBus, Memory compaction.
  7. Ensure default script runner is active (`ensureDefaultScriptRunnerActive`).
  8. Block until SIGINT/SIGTERM, then shut down in reverse order.
- `--safe` flag — run in safe mode (skill sandboxing maximised).
- `--model` flag — override the default LLM model for the chat agent.

## System Fit
The only place that starts all subsystems. An error in AegisHub launch is fatal here; all other daemon components depend on the message hub being live.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/api` — HTTP API server
- `github.com/PixnBits/AegisClaw/internal/ipc` — IPC bridge and vsock
- `github.com/PixnBits/AegisClaw/internal/sandbox` — Firecracker launcher
