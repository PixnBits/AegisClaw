# Package: cmd/guest-agent

## Overview
`cmd/guest-agent` is the vsock-based JSON-RPC server that runs as PID 1 inside every Firecracker microVM managed by AegisClaw (skill VMs, worker VMs, review VMs). It is the sole trusted execution environment inside the guest: the host daemon never runs commands directly — it always routes through the guest agent.

## Security Contracts
- All file operations are confined to `/workspace`; path traversal is rejected.
- Secrets are written to `/run/secrets/<name>` (0600) on a tmpfs mount.
- Command execution timeout: 60s default, maximum 10 minutes.
- Payload size capped at 10 MiB.
- The binary has no dependencies on AegisClaw internal packages — it is self-contained for rootfs embedding.

## Supported Request Types
| Type | Description |
|------|-------------|
| `exec` | Execute a command under `/workspace` |
| `file.read` | Read a file under `/workspace` |
| `file.write` | Write a file under `/workspace` |
| `file.list` | List a directory under `/workspace` |
| `status` | Return hostname, uptime, workspace status |
| `secrets.inject` | Write secrets to `/run/secrets` tmpfs |
| `secrets.refresh` | Overwrite secrets in place (zero-downtime rotation) |
| `tool.invoke` | Dispatch a tool call to the skill's tool handler |
| `review.execute` | Run a court review execution inside the VM |
| `chat.message` | Process a chat message (worker agent loop) |

## Files

| File | Description |
|------|-------------|
| `main.go` | Complete guest agent implementation: vsock server, all request handlers, system setup |
| `main_test.go` | Unit tests for `decodeStructuredChatResponse` (native tool calls, JSON fences, plain text) |
