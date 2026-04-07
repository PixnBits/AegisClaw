# AegisClaw Network & Secrets Architecture Specification

**Status**: Draft (for implementation guidance)  
**Version**: 0.2  
**Date**: April 2026  
**Audience**: Developers implementing hostname-based egress control and secret injection wiring.  
**Repository**: https://github.com/PixnBits/AegisClaw  

This document closes the two **blocking gaps** identified in the deep-dive:

1. **Hostname-based egress control** for real-world skills (Discord, Telegram, GitHub, Slack, etc.).
2. **Secret injection** at skill activation time.

It preserves AegisClaw’s paranoid-by-design principles: Firecracker microVM isolation, vsock-only communication, default-deny everywhere, read-only rootfs, minimal host attack surface, and auditable Governance Court review.

---

## 1. Goals

- Enable practical skills that require outbound HTTPS/WSS without exposing broad IP ranges or allowing arbitrary network access.
- Make secret handling fully automatic at activation while keeping values out of proposals, logs, and guest memory until explicitly injected.
- Keep proposals **semantic and reviewable** (FQDNs, not opaque CIDRs).
- Minimize changes to existing nftables path; add a new, auditable proxy path modeled on the existing LLM proxy (`proxy.go`).
- Provide clear guidance for the Agent and Court reviewers so first-draft proposals pass with high probability.

---

## 2. Network Policy Evolution

### Current Limitation
`generateHostRules` in `netpolicy.go` only accepts IPs or CIDRs. FQDNs are rejected. Broad CDN ranges (Cloudflare, Fastly, etc.) are too permissive for CISO/SecurityArchitect review.

### New Design: **Egress Proxy Mode** (Primary) + Optional Direct Mode

Introduce an **egress proxy** (new file: `egressproxy.go`) that mirrors the LLM proxy pattern:

- Listens on vsock (recommended port **1026** to parallel LLM proxy on 1025).
- Skill VMs with network access route **all outbound TCP/443** (and 80 if explicitly allowed) to this proxy via policy routing or explicit client configuration (`HTTPS_PROXY` / `http.Proxy`).
- The proxy inspects:
  - **SNI** from TLS ClientHello (for HTTPS/WSS) — no TLS termination.
  - **Host** header (for plain HTTP, if ever allowed).
- Enforces against the per-skill `allowed_hosts` list (now supports FQDNs, exact match or configurable suffix/wildcard).
- On match: establish transparent tunnel (CONNECT-style or splice) to the real destination.
- On mismatch: drop + log with prefix `aegis-egress-drop-<sandboxID>: <attempted-host>`.
- All connections logged with skill ID, FQDN, outcome, and timestamp for audit.

**Benefits**
- Handles dynamic/CDN IPs perfectly (Discord, GitHub, Telegram, etc.).
- Preserves end-to-end encryption.
- Court reviewers see intent-based policy (`api.discord.com`) instead of broad ranges.
- Consistent architecture with LLM proxy → easy code reuse and auditing.
- Default-deny remains enforced at nftables level; proxy is the only allowed outbound path.

**Fallback**: Direct nftables mode (existing IP/CIDR path) remains for skills that truly need raw TCP/UDP and can tolerate static IPs. New field `egress_mode: "proxy" | "direct"` (default `"proxy"` for new skills).

### Updated Data Models

**In `proposal.go`** (and mirrored in `spec.go` for sandbox):

```go
type ProposalNetworkPolicy struct {
    DefaultDeny      bool     `json:"default_deny"`      // MUST be true (hard invariant)
    AllowedHosts     []string `json:"allowed_hosts"`     // FQDNs (preferred) or IPs/CIDRs
    AllowedPorts     []uint16 `json:"allowed_ports"`
    AllowedProtocols []string `json:"allowed_protocols"` // usually ["tcp"]
    EgressMode       string   `json:"egress_mode"`       // "proxy" (default) or "direct"
}

type SkillCapabilities struct {
    Network bool     `json:"network"`
    Secrets []string `json:"secrets"`
    // ... existing fields
}
```

**Validation rules** (enforced in proposal submission + Court review + builder):
- `DefaultDeny` must be `true`.
- No `0.0.0.0/0`, `::/0`, or overly broad CIDRs.
- `AllowedPorts` should be minimal (typically `[443]`).
- `AllowedProtocols` typically `["tcp"]`.
- For proxy mode: FQDNs preferred; reject suspicious patterns.
- `security_considerations` field (existing or new) must describe network needs.

### Protocol-Specific Guidance for Common Skills

**Discord Messaging Skill** (example):
- Endpoints:
  - REST API: `https://discord.com/api/...` and `https://api.discord.com/...`
  - Gateway (WSS): `wss://gateway.discord.gg/...` (port 443)
  - CDN: `https://cdn.discordapp.com/...`
- Proposal snippet:
  ```json
  "network_policy": {
    "default_deny": true,
    "allowed_hosts": [
      "discord.com",
      "api.discord.com",
      "gateway.discord.gg",
      "cdn.discordapp.com"
    ],
    "allowed_ports": [443],
    "allowed_protocols": ["tcp"],
    "egress_mode": "proxy"
  }
  ```

**Telegram Bot API**:
- Primary: `https://api.telegram.org/...`
- Proposal: `allowed_hosts: ["api.telegram.org"]`, port 443, tcp.

**GitHub REST API**:
- Primary: `https://api.github.com/...`
- Proposal: `allowed_hosts: ["api.github.com", "github.com"]` (if needed for redirects).

**General rule for Agent**: Use the exact hostname(s) the client library or code will connect to. Prefer specific subdomains over wildcards unless justified.

---

## 3. Secret Management Completion

### Current State
- Vault (`vault.go`): Age-encrypted at rest, daemon-only access.
- Guest agent (`main.go`): `secrets.inject` handler writes to `/run/secrets/<name>` (tmpfs, mode 0400).
- CLI: `aegisclaw secrets add/list/rotate` (secure prompt, never in args/history).
- Gap: Injection not wired in `makeSkillActivateHandler` (D5 comment).

### Required Changes

1. **Add Vault to runtime state**
   - Extend `runtimeEnv` (or equivalent daemon struct) with:
     ```go
     Vault *vault.Vault
     ```
   - Open once at daemon startup (`aegisclaw start`). Fail daemon start if vault cannot be opened.

2. **Wire injection in `start.go` (`makeSkillActivateHandler`)**
   After successful VM start and before marking ready:

   ```go
   // After VM is started and registered
   if len(fullProposal.SecretsRefs) > 0 && env.Vault != nil {
       var items []SecretItem
       missing := []string{}

       for _, ref := range fullProposal.SecretsRefs {
           val, err := env.Vault.Get(ref)
           if err != nil {
               if errors.Is(err, vault.ErrNotFound) {
                   missing = append(missing, ref)
                   continue
               }
               return fmt.Errorf("vault error for secret %s: %w", ref, err)
           }
           items = append(items, SecretItem{Name: ref, Value: string(val)})
       }

       if len(missing) > 0 {
           // Recommended: soft fail with warning (skills can be "degraded")
           log.Warnf("Skill %s activated with missing secrets: %v. Tools requiring them will fail.", skillName, missing)
           // Alternative (paranoid mode): hard fail via config flag
       }

       if len(items) > 0 {
           injectReq := map[string]interface{}{
               "id":      uuid.New().String(),
               "type":    "secrets.inject",
               "payload": SecretInjectPayload{Secrets: items},
           }
           if err := env.Runtime.SendToVM(ctx, sandboxID, injectReq); err != nil {
               return fmt.Errorf("failed to inject secrets: %w", err)
           }
           log.Infof("Injected %d secret(s) into skill VM %s", len(items), skillName)
       }
   }
   ```

3. **Pre-activation verification** (recommended addition to `aegisclaw skill activate`)
   - Check all `secrets_refs` exist in vault.
   - Output clear message if missing:
     > "Missing required secret(s): DISCORD_BOT_TOKEN  
     > Add with: `aegisclaw secrets add DISCORD_BOT_TOKEN --skill discord-messenger`"

4. **Secret re-reading and rotation**
   - Skill code **must re-read** `/run/secrets/<name>` on each tool invocation (or on a new `secrets.refresh` message). Do not cache long-term.
   - `secrets rotate <name>` updates vault.
   - Add `secrets.refresh` command/message to re-inject to a running VM without full restart (nice-to-have, low priority initially).

### Skill VM Expectations
- Read secrets exclusively from `/run/secrets/<name>`.
- Never log, persist, or expose secret values.
- Document in `security_considerations`.

---

## 4. Agent System Prompt Updates (`chat_handlers.go`)

Add to `buildDaemonSystemPrompt`:

- Full proposal JSON schema excerpt (including new `network_policy` and `egress_mode`).
- Concrete Discord example (using FQDNs + `"egress_mode": "proxy"`).
- Explicit instructions:
  > "For any skill requiring network access:  
  > - Set `capabilities.network: true`  
  > - Declare precise `allowed_hosts` as FQDNs (e.g., api.discord.com)  
  > - Use minimal ports (usually [443]) and protocols [\"tcp\"]  
  > - Set `egress_mode: \"proxy\"` (default)  
  > - List required secrets in both `capabilities.secrets` and `secrets_refs`  
  > - Instruct user to add secrets via CLI before activation  
  > - Never include secret values in the proposal"

After `proposal.create_draft`, the agent should output user guidance for secrets.

---

## 5. Governance Court Impact

Reviewers (especially CISO and SecurityArchitect) will now see:
- Clear FQDN intent instead of broad ranges.
- `egress_mode: "proxy"` → strong assurance of SNI enforcement.
- Explicit secret references matched to capabilities.

Rejection triggers remain (broad access, missing `default_deny`, undeclared capabilities, etc.). Add guidance in persona prompts that proxy mode is preferred and reduces risk.

---

## 6. Implementation Roadmap (Prioritized)

1. **Blocking – Secret injection** (lowest risk)
   - Add `Vault` to runtimeEnv.
   - Implement wiring in `makeSkillActivateHandler`.
   - Add pre-activation check to CLI.

2. **Blocking – Egress Proxy**
   - Implement `egressproxy.go` (vsock listener, SNI inspection, allowlist enforcement).
   - Update network setup in `firecracker.go` / `applyNetworkPolicy()` to route 443 traffic to proxy when `egress_mode == "proxy"`.
   - Extend data models and validation.
   - Update nftables rules to allow only proxy-bound traffic.

3. **Agent & UX**
   - Update system prompt with schema + example.
   - Improve proposal creation flow to surface secret instructions.

4. **Polish**
   - `secrets.refresh` support.
   - Dynamic nftables sets as optional fallback.
   - Enhanced logging/audit for proxy connections.

---

## 7. Example: Complete Discord Proposal (Post-Implementation)

```json
{
  "title": "Discord Messaging Gateway Skill",
  "skill_name": "discord-messenger",
  "description": "Sends and receives messages via Discord bot API using bot token secret. All egress via host SNI-enforcing proxy. No message content persisted.",
  "tools": [
    {"name": "send_message", "description": "Send a message to a channel"},
    {"name": "list_channels", "description": "List accessible channels"}
  ],
  "capabilities": {
    "network": true,
    "secrets": ["DISCORD_BOT_TOKEN"]
  },
  "secrets_refs": ["DISCORD_BOT_TOKEN"],
  "network_policy": {
    "default_deny": true,
    "allowed_hosts": ["discord.com", "api.discord.com", "gateway.discord.gg", "cdn.discordapp.com"],
    "allowed_ports": [443],
    "allowed_protocols": ["tcp"],
    "egress_mode": "proxy"
  },
  "security_considerations": "Bot token injected at runtime to /run/secrets/DISCORD_BOT_TOKEN (tmpfs, 0400). Re-read on each use. All outbound traffic SNI-validated by host egress proxy. Rate limiting client-side."
}
```
