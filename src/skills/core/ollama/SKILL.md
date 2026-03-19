# Ollama Skill v2.1.4 – Hardened Process Model

**Status:** Core bootstrap skill  
**Single source of truth:** ARCHITECTURE.md v2.1+, PRD.md v2.1+, CORE_IMPLEMENTATION_PATTERNS.md, this file  
**Must enforce** every v2.1+ invariant, especially the dual-process model below.

## Purpose
Manages a local Ollama instance for model serving and pulls while acting as the secure, message-hub-only bridge. The Go binary is the sole control-plane citizen.

## Critical Container Architecture & Process Model (NON-NEGOTIABLE – v2.1.4)

**The compiled Go binary MUST be PID 1 (ENTRYPOINT).**  
The Go program is responsible for:

1. Starting and managing the real `ollama serve` process as a child (background or exec).
2. Connecting to `message-hub` via TCP control channel (using the exact retry + bufio.Scanner pattern from CORE_IMPLEMENTATION_PATTERNS.md).
3. Bridging **every** structured message between message-hub and the internal Ollama HTTP API at `http://127.0.0.1:11434`.
4. Graceful shutdown: propagate SIGTERM to the child Ollama process.
5. Health monitoring: verify both TCP connection and `/api/tags` before declaring ready.

**Never** allow the official Ollama image to run as the main process. The container’s entrypoint **must** be our Go binary (e.g. `/usr/local/bin/ollama-skill`).

## Critical Container Architecture & Process Model (NON-NEGOTIABLE – v2.1.4)

The container MUST run as a **dual-process supervisor** where:

- The compiled Go binary (built from the skill's source) is the **only entrypoint / PID 1 process**.
- The Go program is responsible for:
  - Starting the real Ollama server as a child subprocess (equivalent to running `ollama serve` in the background).
  - Connecting to message-hub using the exact TCP client retry + bufio.Scanner forever loop pattern defined in CORE_IMPLEMENTATION_PATTERNS.md.
  - Bridging **all** incoming structured JSON messages from message-hub into local HTTP API calls to Ollama running on `http://127.0.0.1:11434` inside the same container.
  - Forwarding Ollama responses (or errors) back as structured JSON messages to message-hub.
  - Gracefully propagating SIGTERM/SIGINT to the child Ollama process on container shutdown.
  - Periodically verifying that the child Ollama process is healthy (e.g. via `/api/tags` endpoint).

**No other process model is acceptable.**  
The official `ollama/ollama` image MUST NOT be used as the base entrypoint — it must be used only as the base image, with our Go binary overlaid as the true main process.

## Dockerfile Generation Requirements (MANDATORY – describe these invariants exactly)

When generating the Dockerfile for this skill, the following rules **must** be followed verbatim:

- Use multi-stage build:
  - Stage 1: Build the Go binary using a recent golang:*-alpine base.
    - Copy the skill source code.
    - Build with trimming and stripping flags: `-trimpath -ldflags="-s -w"`.
    - Output binary to a known path (e.g. `/ollama-skill` or `/usr/local/bin/ollama-skill`).
  - Stage 2: Start from `ollama/ollama:latest` (or a pinned version if specified).
    - Install minimal runtime dependencies if needed (e.g. `ca-certificates` via apk).
    - Copy the compiled Go binary from stage 1 into the final image (e.g. `/usr/local/bin/ollama-skill`).
- Set the **ENTRYPOINT** to the copied Go binary only:
  - `ENTRYPOINT ["/usr/local/bin/ollama-skill"]` (or equivalent path).
  - Do **not** use `CMD ["ollama", "serve"]` or any variant that makes Ollama PID 1.
- Do **not** include any `EXPOSE 11434` instruction.
- Do **not** add any `ports:` mapping in the compose.yaml service definition for this skill.
- Ensure the final image remains as small and minimal as possible while preserving the dual-process model.

## Networking & Exposure Rules (Critical – zero surprises)

- Ollama's HTTP API **must** bind **only** to `127.0.0.1:11434` inside the container (never 0.0.0.0).
  - This must be enforced by the Go wrapper when starting the child `ollama serve` process (pass `--host 127.0.0.1` or equivalent flag if Ollama supports it, or rely on default localhost binding).
- No container port publishing is allowed (`ports:` forbidden in compose.yaml).
- No other skill or external process may ever connect directly to port 11434 — all access is mediated exclusively through this skill's Go wrapper → message-hub.

## Implementation Wiring & Compliance
You MUST read and follow CORE_IMPLEMENTATION_PATTERNS.md verbatim before generating any code.  
In particular:
- Use the exact TCP client retry + active bufio.Scanner loop pattern for message-hub connection.
- Include the mandatory control-plane pattern comments in the generated Go code.
- At the end of generation, output the filled compliance checklist from CORE_IMPLEMENTATION_PATTERNS.md.

**Additional requirements specific to this skill:**
- The Go main() function must:
  - Start the child Ollama server process **before** entering the TCP message loop.
  - Use a standard HTTP client pointed at `http://127.0.0.1:11434` for all Ollama interactions.
  - Implement basic health monitoring of the child process.
  - Log startup sequence (Go binary start → Ollama child start → TCP connect success) to stdout for debugging visibility.
- Every bridge action (message received → Ollama call → response sent) must trigger a structured audit event forwarded to message-hub.

## Trivial Audit Guarantee
After successful registration and startup:
```bash
grep -E '"ollama"|ENTRYPOINT|ollama serve|127.0.0.1:11434|PID 1|child process' shared/audit/aegisclaw.log
```
should reveal that the Go wrapper is correctly running as PID 1 and that Ollama is managed internally with no external port exposure.

## Networking & Exposure Rules (Critical – zero surprises)

- Ollama API port 11434 **MUST NOT** be published (`ports:` section forbidden in compose.yaml).
- No `EXPOSE 11434` in Dockerfile.
- Inside the container, Ollama must bind **only** to `127.0.0.1:11434` (never 0.0.0.0).
- Other skills **never** talk directly to `ollama:11434` — all calls go through message-hub → this Go wrapper.
- Outbound policy remains narrow (only registry.ollama.ai for pulls).

## Network Policy (v2.1 Mandatory – unchanged)
```json
{
  "name": "ollama",
  "required_mounts": ["ollama/models:rw"],
  "network_policy": {
    "outbound": "allow_list",
    "domains": ["registry.ollama.ai", "ollama.com"],
    "ports": [443],
    "network_mode": "aegisclaw-net"
  },
  "network_needed": true
}
```

## Resource Exception (audited, ollama-only)
`mem_limit: 16g` + `shm_size: 1g` (applied only to this service in compose.yaml).

## Communication (Strict – hub-only)
All requests arrive via message-hub. The Go binary translates them into local HTTP calls to `127.0.0.1:11434` and returns structured responses.

**Example incoming message:**
```json
{
  "from": "user-agent",
  "to": "ollama",
  "content": {
    "action": "pull|generate|chat|list",
    "model": "...",
    "prompt?": "..."
  }
}
```

## Implementation Wiring
You MUST read and follow CORE_IMPLEMENTATION_PATTERNS.md verbatim before generating any code.  
Copy the exact skeletons where applicable (especially TCP client retry + active scanner loop).  
At the end of generation, output the filled compliance checklist from that file.

**Additional requirements for this skill only:**
- Go main() must launch `ollama serve` **before** entering the TCP scanner loop.
- Use `http.Client` with base URL `http://127.0.0.1:11434` for all Ollama API interactions.
- Include a healthcheck that verifies both the control-plane TCP connection and the local Ollama endpoint.
- Every bridge action must be forwarded as an audit event (aegisclaw writes the immutable log).

## Trivial Audit Guarantee
After registration:
```bash
grep -E '"ollama"|ENTRYPOINT|ollama serve|127.0.0.1:11434|PID 1' shared/audit/aegisclaw.log
```
shows exactly that the Go wrapper is running as the control plane and the port is internal-only.

This SKILL.md is now the binding contract. Any generated Dockerfile or Go code that violates the “Go binary = PID 1 + internal child” rule **must** be rejected during sandbox vetting by aegisclaw.
