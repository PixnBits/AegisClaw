# cmd/guest-agent/main.go

## Purpose
Entry point and complete implementation of the AegisClaw guest agent — a vsock-based JSON-RPC server that runs as PID 1 (or init) inside every skill and worker Firecracker microVM. The host daemon communicates with it over vsock to execute commands, read/write files, inject secrets, invoke tools, and run chat messages.

## Key Constants
- `vsockPort = 1024` — well-known guest-agent vsock port.
- `workspaceDir = "/workspace"` — all file operations are confined to this directory.
- `secretsDir = "/run/secrets"` — tmpfs directory for injected secrets.
- `maxPayloadLen = 10 MiB` — caps request payload size.

## Key Types
- `Request` / `Response` — JSON envelope with `id`, `type`, `payload`/`data`, `success`, `error`.
- `ExecPayload` / `ExecResult` — command execution: `command`, `args`, `dir`, `timeout_secs` → `exit_code`, `stdout`, `stderr`.
- `FileReadPayload` / `FileWritePayload` / `FileListPayload` / `FileEntry` — workspace file operations.
- `StatusData` — hostname, uptime, workspace-mounted flag, PID.
- `SecretInjectPayload` / `SecretItem` — set of named secrets to write to tmpfs.

## Key Functions
- `main()` — mounts essential filesystems, creates `/workspace`, retries vsock listen 20 times (vsock device may not be ready at PID 1 start), falls back to TCP, handles SIGTERM.
- `handleConnection(ctx, conn)` — per-connection JSON decode/encode loop.
- `dispatch(ctx, req)` — routes to handler by `req.Type`.
- `handleExec` — runs `command` with args in `dir` (must be under `/workspace`); timeout 60s default, capped at 10min; stdout/stderr capped at `maxPayloadLen`.
- `handleFileRead` / `handleFileWrite` / `handleFileList` — all enforce `isUnderWorkspace` path validation; `handleFileWrite` creates parent directories automatically.
- `handleStatus` — returns hostname, `/proc/uptime`, workspace mount status.
- `handleSecretsInject` — writes `SecretItem` values as files under `/run/secrets/<name>` (0600); handles both `secrets.inject` and `secrets.refresh`.
- `handleToolInvoke` — dispatches a tool call to the skill's registered tool handler.
- `handleReviewExecute` — runs the court review execution path inside the VM.
- `handleChatMessage` — processes a chat message inside the VM (worker agent loop).
- `isUnderWorkspace(absPath)` — path traversal guard.
- `configureNetwork()` / `mountEssentialFS()` — low-level system setup (TCP fallback; proc/sysfs/devtmpfs/tmpfs mounts).
- `listenVsock(port)` — AF_VSOCK listener with TCP fallback.
- `vsockConn` / `vsockListener` / `vsockAddr` — AF_VSOCK net.Conn/Listener/Addr wrappers (same pattern as AegisHub and AegisPortal).

## System Fit
The trusted execution environment inside each microVM. All capability enforcement on the guest side is implemented here (e.g. workspace confinement, secret file permissions). The host daemon never executes commands directly — it always sends them through the guest agent.

## Notable Dependencies
- `golang.org/x/sys/unix` — AF_VSOCK socket operations
- Standard library only (no internal AegisClaw packages; the binary is self-contained for rootfs embedding)
