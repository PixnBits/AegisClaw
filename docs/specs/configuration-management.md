# Configuration Management Specification

## Overview
AegisClaw uses a layered configuration system that is simple for users but flexible for advanced scenarios.

## Configuration Sources (in order of precedence)

1. **Defaults** (compiled into binary)
2. `~/.aegis/config.yaml` (user config)
3. Environment variables (`AEGIS_*`)
4. CLI flags (highest priority)

## Main Configuration File

**Location:** `~/.aegis/config.yaml`

### Example `config.yaml`

```yaml
# Global defaults
default_model: "llama3.2:3b"
default_temperature: 0.7
max_context_tokens: 128000

# Resource limits
max_concurrent_agents: 8
max_memory_per_agent_mb: 2048
max_background_tasks: 20

# Sandbox
sandbox_backend: "firecracker"   # or "docker"

# Default agent profile
default_agent_profile: "default"

# Per-agent model / temperature overrides
agents:
  researcher:
    model: "claude-3.5-sonnet"
    temperature: 0.6
  analyst:
    model: "llama3.2:8b"
    temperature: 0.3

# Logging
log_level: "info"
log_format: "structured"

# Web Portal
web_port: 8080
web_host: "localhost"
```

## Loading & Validation

- Host Daemon loads and validates config on startup
- Clear errors + suggestions via `aegis doctor`
- Changes require `aegis restart`

## Environment Variable Mapping

- `AEGIS_DEFAULT_MODEL=llama3.2`
- `AEGIS_AGENTS_RESEARCHER_MODEL=claude-3.5-sonnet`
- `AEGIS_WEB_PORT=9090`

## Related Documents
- `../host-daemon.md`
- `../agent-customization.md`
- `../secrets-vault.md`

## Traceability
**Driven by:**
- Need for easy user customization
- Support for per-agent overrides
- Lessons from v1