# AegisClaw Bootstrap Prompt – v2.1 (2026-03-11)

**This is the canonical v2.1 bootstrap prompt.** Use it **exactly** as-is with a top-tier coding LLM (Claude 3.5 Sonnet or better, Cursor, Aider) to generate the initial `aegisclaw.go` binary. 

**You are the Lead Security Architect and Principal Go Engineer.** Your generated code is the microscopic Trusted Computing Base. It must be paranoid-by-design, minimal, auditable, and enforce every invariant in PRD.md v2.1 and ARCHITECTURE.md v2.1. 

**Single Source of Truth:** PRD.md v2.1 and ARCHITECTURE.md v2.1 (2026-03-11). The generated `aegisclaw.go` must implement 100% of the architecture described there. Deviations are forbidden and must cause explicit runtime rejection + audit entry.

**NON-NEGOTIABLE SECURITY INVARIANTS** (enforce these in code with comments and runtime checks):

1. **Control Channel**: Listen *exclusively* on `127.0.0.1:7124` (or AegisClaw_CONTROL_PORT env). Pure JSON-over-TCP. No WebSockets, no Unix sockets (permanently banned), no HTTP for MVP. Only `message-hub` (via host.internal:host-gateway) may connect. Validate incoming connections strictly.

2. **Docker Network**: Create and use dedicated `aegisclaw-net` for *all* containers. Never use `host` network_mode. Reject any skill attempting it.

3. **Default Container Runtime Profile** (apply to EVERY service in compose.yaml):
   - network: aegisclaw-net
   - read_only: true
   - tmpfs: /tmp
   - cap_drop: [ALL]
   - security_opt: [no-new-privileges:true]
   - mem_limit: 512m
   - cpu_shares: 512
   - ulimits: nproc=64, nofile=64
   - restart: unless-stopped
   - extra_hosts: ["host.internal:host-gateway"] *ONLY* for message-hub

4. **Skill Registration Metadata** (mandatory, enforced):
   ```json
   {
     "name": "skill-name",
     "required_mounts": ["sources:ro", "outputs:rw"],
     "network_policy": {
       "outbound": "none" | "allow_list",
       "domains": ["api.example.com", "*.example.org"],
       "ports": [443],
       "network_mode": "aegisclaw-net"
     },
     "network_needed": false,
     "hash": "sha256:................................................",
     "timestamp": "2026-03-11T13:45:22Z",
     "previous_hash": "sha256:................................................"
   }
   ```
   Coder skill (generated later) must always produce this. AegisClaw rejects anything missing or invalid.

5. **Registration Lifecycle** (exact order, all audited):
   1. Receive generate request via TCP.
   2. Route to coder skill.
   3. Receive bundle (code, Dockerfile, metadata).
   4. Sandbox compile in temp container: go build, go vet, static analysis.
   5. Validate network_policy, required_mounts, no host network, no undeclared outbound.
   6. Append *only* the precise service definition to compose.yaml using Default Profile + declared mounts/policy.
   7. `docker compose up -d`
   8. Append immutable audit entry with full network_policy.

6. **Rejection Rules** (hard, logged):
   - Any `network_mode: host`
   - Missing or invalid network_policy
   - Broad mounts (only explicitly declared)
   - Undeclared outbound attempts (scan code for net/http if possible in MVP)

7. **Audit Trail**: `./shared/audit/aegisclaw.log` is append-only JSONL. Every entry:
   - ts, actor, action, skill, network_policy (full object), mounts, hash, status, previous_hash (for chaining with SHA-256).
   Trivial auditing: `grep -E '"network_policy|outbound|domains"' shared/audit/aegisclaw.log` shows entire swarm connectivity. Implement hash chaining.

8. **Shared Dir & Mounts**: Only purpose-driven selective mounts. Never mount entire shared/ . message-hub gets no filesystem control mounts (TCP only).

9. **Minimalism**: <20 MiB idle RAM. Dependencies: stdlib + `github.com/docker/docker/client` (v26+). No other libs. Static binary. Use Docker API where possible, fallback to exec `docker compose` for simplicity in MVP.

**Project Structure to Generate** (output as separate code blocks):

- `go.mod`
- `aegisclaw.go` (complete main logic)
- Initial `compose.yaml` template handling (aegisclaw manages it)
- Any helper structs for JSON protocol, registry, audit.

**On Startup Sequence** (hardcoded):
- Verify Docker.
- mkdir -p ./shared/{sources,builds,outputs,logs,audit,ollama/models}
- Create `aegisclaw-net` if missing.
- Load or init registry.json and audit.log.
- Generate/overwrite initial compose.yaml with the four core skills (message-hub, llm-caller, ollama, coder) from `./src/skills/core/*/Dockerfile` paths. Apply Default Profile + narrow outbound for llm-caller/ollama.
- `docker compose up -d`
- Start TCP JSON listener.
- Log every startup action to audit.

**Communication Protocol**: Define simple JSON message types for:
- generate_skill {skill_name, prompt}
- register_skill {metadata, code_bundle}
- route to message-hub only for skill-to-skill.

**Implementation Notes**:
- Use bufio or net for TCP handling.
- For bootstrap, support a simple stdin mode initially that sends JSON to the local TCP port (for user to paste prompts).
- Treat all generated code as potentially malicious: sandbox everything.
- Include SHA-256 helpers for audit chaining and code verification.
- Make `compose.yaml` edits atomic (backup first).
- On any violation, panic with audit entry and clear error.

**Output ONLY**:
- Full file contents in fenced code blocks labeled with filenames.
- No explanations outside the code blocks.
- Add extensive comments in aegisclaw.go referencing the invariants.

The resulting binary must make the first `./aegisclaw` run create a fully isolated, auditable swarm ready for the next bootstrap step (generating additional skills via the control channel). Security and easy auditing above all.

Generate the code now.
