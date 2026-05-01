# Package `llm` — Overview

## Purpose
Package `llm` is the LLM infrastructure layer for AegisClaw. It mediates every interaction between the system and a locally running Ollama instance, providing security boundaries, structured output enforcement, model lifecycle management, persona-based routing, and cross-model verification.

## Architecture

```
Skill VM (Firecracker)
  │  vsock port 1025 (LLM inference)
  │  vsock port 1026 (egress HTTPS)
  ▼
┌─────────────────────────────────────────────────────────┐
│ OllamaProxy (proxy.go)     EgressProxy (egressproxy.go) │
│  • model allowlist gate     • SNI allowlist gate         │
│  • payload cap              • transparent TLS splice     │
│  • audit log writes         • per-VM FQDN policy         │
└───────────────┬─────────────────────────────────────────┘
                │  localhost HTTP
                ▼
          Ollama (:11434)
                ▲
┌───────────────┴──────────────────────────────────────────┐
│ Client (ollama.go)                                        │
│  Generate · Chat · List · Show · Pull · Healthy           │
└───┬──────────────────────────────────────────────────────┘
    │
    ├── Enforcer (enforcer.go)
    │    Retry + temperature decay + JSON schema validation
    │    OutputSchema · ReviewSchema · CodeGenSchema
    │
    ├── Verifier (verifier.go)
    │    Cross-model consensus · VerifyStandard / VerifyCritical
    │    Majority vote · Discrepancy detection · EscalateToHuman
    │
    ├── Manager (manager.go)
    │    Model lifecycle: ListStatus · Verify · Update · SyncKnownGood
    │    KnownGoodModels (approved model list with persona tags)
    │
    ├── ModelRegistry (registry.go)
    │    Thread-safe JSON-file store of ModelEntry{Name, SHA256, Tags}
    │
    ├── Router (router.go)
    │    Persona → {models, temperature, mode, schema} resolution
    │    Loaded from YAML file or directory of persona files
    │
    └── IsolationEnforcer (isolation.go)
         Caller-type and sandbox-context policy checks
```

## Key Files

| File | Role |
|---|---|
| `ollama.go` | HTTP client and all Ollama API types |
| `proxy.go` | vsock LLM proxy; model allowlist; streaming decoder; audit logging |
| `egressproxy.go` | vsock HTTPS egress proxy; SNI allowlist; transparent splice |
| `enforcer.go` | JSON schema enforcement and retry logic around Ollama calls |
| `verifier.go` | Cross-model consensus verification for critical decisions |
| `manager.go` | Model lifecycle (list, verify, pull, seed known-good) |
| `registry.go` | Persistent model metadata store |
| `router.go` | Persona-to-model routing from YAML config |
| `isolation.go` | Caller-type and sandbox context policy enforcement |
| `proxy_inprocesstest.go` | Test-only shim (build tag `inprocesstest`) bypassing vsock transport |

## Security Properties
- **Allowlist-only model access**: every inference request through `OllamaProxy` is checked against a compile-time model allowlist.
- **No TLS termination**: `EgressProxy` reads only the ClientHello SNI; all end-to-end encryption is preserved.
- **Kernel callers blocked**: `IsolationEnforcer` unconditionally blocks `"kernel"` and `"cli"` caller types from reaching Ollama.
- **Tamper-evident audit trail**: `OllamaProxy` writes `llm.infer` entries to the `internal/kernel` log for every inference call.
- **Cross-model consensus**: `Verifier` requires agreement across ≥2 models for CISO-level decisions; disagreements trigger `EscalateToHuman`.

## Notable Dependencies
- `go.uber.org/zap` – structured logging throughout.
- `gopkg.in/yaml.v3` – persona routing config parsing.
- `github.com/PixnBits/AegisClaw/internal/kernel` – tamper-evident audit log (used in `proxy.go`).
