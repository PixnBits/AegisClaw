# Package `internal/config` — Daemon Configuration

## Purpose
Defines the single `Config` struct that is the authoritative schema for all daemon configuration. Loaded from `~/.config/aegisclaw/config.yaml` via Viper at daemon startup and passed down to every subsystem.

## Files

| File | Description |
|---|---|
| `config.go` | `Config`, `GatewayChannelConfig`, `Load()`, path-validation helpers |

## Key Abstractions

- **`Config`** — root struct with deeply nested sections for each subsystem, all tagged with `yaml` and `mapstructure` for Viper binding
- **`GatewayChannelConfig`** — mirrors `gateway.ChannelConfig` without creating an import cycle

## Configured Subsystems

| Section | Controls |
|---|---|
| `Firecracker` / `Jailer` | Binary paths |
| `Sandbox` | State dir, isolation mode (`firecracker` only), kernel image |
| `Court` | Persona YAML dir, session dir |
| `Builder` | Rootfs template, SBOM dir, concurrency caps |
| `Ollama` | Endpoint, timeout, models |
| `Daemon` | Unix API socket path |
| `Composition` | Versioned manifest directory |
| `Agent` | Rootfs path, structured-output flag |
| `Memory` | Dir, embedding model, size cap, PII redaction |
| `EventBus` | Dir, max timers, max subscriptions |
| `Dashboard` | Enabled flag, listen address |
| `Gateway` | Enabled flag, channel adapter list |
| `Registry` | ClawHub-compatible registry URL |

## How It Fits Into the Broader System
`cmd/daemon` calls `config.Load()` at startup and fans out the resulting `Config` to every subsystem constructor. No subsystem imports another to read its configuration — all cross-cutting settings are routed through this package.

## Notable Dependencies
- `github.com/spf13/viper`
- `go.uber.org/zap`
