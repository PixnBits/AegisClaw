# isolation.go

## Purpose
Enforces access-control rules that govern which callers may contact the Ollama inference endpoint and from which execution contexts. Acts as a policy gate preventing, for example, the kernel process or CLI from calling Ollama directly.

## Key Types / Functions
- **`IsolationPolicy`** – configures whether sandbox execution is required and which hosts/ports are permitted.
- **`DefaultIsolationPolicy()`** – returns the recommended production policy: sandbox-required, host `127.0.0.1`, port `11434`.
- **`IsolationContext`** – carries caller metadata (`InSandbox`, `SandboxID`, `CallerType`).
- **`IsolationError`** – structured error type with `Reason`, `CallerType`, and `Endpoint` fields.
- **`IsolationEnforcer`** – performs the policy check.
- **`IsolationEnforcer.Check(ctx, endpoint)`** – blocks `"kernel"` callers unconditionally; enforces sandbox requirement; validates host/port against allowlists.
- **`ValidateNetworkPolicy(defaultDeny, allowedHosts, allowedPorts)`** – validates a VM network policy config for LLM access (supports both NoNetwork and Localhost-only modes).
- **`ForbiddenCallerTypes`** / **`IsForbiddenCaller(callerType)`** – hardcoded list of caller types that must never reach Ollama directly (`"kernel"`, `"cli"`).

## System Role
Provides the caller-type and context enforcement layer sitting above the transport. Used during VM setup and in policy validation paths to ensure no prohibited component gains LLM access.

## Notable Dependencies
- `net`, `net/url` – endpoint URL and port parsing.
- `go.uber.org/zap` – blocked-call logging.
