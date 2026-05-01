# `config.go` — Daemon Configuration

## Purpose
Defines the top-level `Config` struct that is populated from `~/.config/aegisclaw/config.yaml` via Viper. Every subsystem that has configurable runtime parameters exposes a nested struct here, keeping configuration centralized and self-documenting.

## Key Types & Functions

| Symbol | Description |
|---|---|
| `Config` | Root struct; loaded by Viper with `mapstructure` tags |
| `GatewayChannelConfig` | Per-channel adapter config mirroring `gateway.ChannelConfig` |
| `Config.Firecracker` | Path to the `firecracker` binary |
| `Config.Jailer` | Path to the `jailer` binary |
| `Config.Sandbox` | State dir, chroot base, kernel image, isolation mode (`firecracker` only) |
| `Config.Court` | Persona YAML directory and session storage directory |
| `Config.Builder` | Rootfs template, workspace base, SBOM output dir, concurrency caps |
| `Config.Ollama` | Endpoint, timeout, model registry, default model |
| `Config.Daemon` | Unix socket path for the host API |
| `Config.Composition` | Directory for versioned composition manifest files |
| `Config.Agent` | Rootfs path, structured-output flag |
| `Config.Memory` | Dir, embedding model, size cap, TTL, PII redaction |
| `Config.EventBus` | Dir, max timers, max subscriptions |
| `Config.Dashboard` | Enable flag and HTTP listen address |
| `Config.Gateway` | Enable flag and `[]GatewayChannelConfig` |
| `Config.Registry` | ClawHub-compatible skill registry URL |
| `Config.Lookup` | Vector DB directory for semantic tool lookup |

The file also contains the `Load()` function (using `github.com/spf13/viper`) and path-validation helpers that prevent directory traversal attacks.

## Role in the System
Consumed by `cmd/daemon` at startup and passed down to every subsystem. It is the single authoritative schema for what can be configured externally.

## Notable Dependencies
- `github.com/spf13/viper` — YAML loading and environment variable binding
- `go.uber.org/zap` — structured logging during load/validation
