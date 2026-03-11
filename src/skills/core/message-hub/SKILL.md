# Message Hub Skill v2.1

Sole IPC router and control plane gateway for the entire SeedClaw swarm. Enforces structured JSON messages, sender validation, and mandatory routing through itself. No skill-to-skill direct communication allowed.

## Capabilities
- Routes all messages (skill↔skill, skill↔seedclaw)
- Validates sender identity and message schema
- Logs every transaction to immutable audit trail
- Enforces no direct networking

## Network Policy (v2.1 Mandatory)
```json
{
  "name": "message-hub",
  "required_mounts": [],
  "network_policy": {
    "outbound": "none",
    "domains": [],
    "ports": [],
    "network_mode": "seedclaw-net"
  },
  "network_needed": true
}
```
- **Only** permitted connectivity: TCP to host.internal:7124 (seedclaw control port) via extra_hosts: ["host.internal:host-gateway"].
- Internet outbound permanently blocked.
- network_mode: seedclaw-net (host forbidden).

## Required Mounts
Minimal/none for control plane (TCP only). Seedclaw adds **no** broad shared/ mounts. If audit logging requires filesystem, use narrow shared/audit:rw ONLY (declared above as [] by default; seedclaw overrides with audit only if needed).

## Default Container Runtime Profile
Fully compliant with ARCHITECTURE.md v2.1: read_only: true, tmpfs: [/tmp], cap_drop: [ALL], security_opt: [no-new-privileges:true], mem_limit: 512m, cpu_shares: 512, network: seedclaw-net, no host network ever.

## Communication Architecture
**ALL** communication (including to seedclaw) routes exclusively through this hub using structured JSON-over-TCP. Other skills NEVER use direct sockets, HTTP to host, or inter-container TCP. Direct access forbidden and rejected at generation/vetting.

## Message Format
**Incoming (from any skill or seedclaw):**
```json
{
  "from": "sender-skill-name",
  "to": "message-hub",
  "content": { ... },
  "metadata": { "policy_hash": "sha256:..." }
}
```
Hub validates `from`, routes to `to`, appends audit entry.

**Outgoing:**
Forwarded with validation.

## Security & Auditing Invariants (NON-NEGOTIABLE)
- Immutable audit logging of EVERY routed message (actor, action, from, to, network_decision, hash) to shared/audit/seedclaw.log.
- Enforces skill isolation and zero direct networking.
- Trivial auditing guarantee: `grep -E '"message-hub|network_policy|outbound"' shared/audit/seedclaw.log` reveals all traffic.
- Rejects any message attempting undeclared networking or bypassing hub.
- Serves as contract for coder skill: ALL generated skills MUST route exclusively via message-hub.
- Least-privilege only. Any violation → registration rejection by seedclaw.

This SKILL.md is the binding specification for v2.1 compliance.
