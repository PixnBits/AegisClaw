# `docs/network-secrets-spec.md` — Summary

## Purpose

Closes two blocking gaps in AegisClaw's security model: **hostname-based egress control** for real-world skills (Discord, Telegram, GitHub, Slack) and **automatic secret injection** at skill activation time. Version 0.2 (April 2026).

## Key Contents

### Problem
- Current network policy uses IP CIDRs — not reviewable or semantic for Court review.
- Secret injection at activation time is not yet automated.

### Network Policy Evolution

**Proposal**: Skills declare egress by FQDN (e.g., `"discord.com"`) in their `SkillSpec`. The proxy resolves FQDNs to IPs at activation and writes nftables rules. Court reviews FQDNs (semantic, not opaque CIDRs).

Architecture:
- New **Egress Proxy** modelled on existing `proxy.go` (LLM proxy pattern).
- Skills connect to a host-side proxy via vsock; proxy resolves and forwards to allowed FQDNs only.
- DNS responses are validated; no wildcard hostnames.

### Secret Injection

**Proposal**: At skill activation, vault derives a per-skill envelope key, decrypts the required secrets, and delivers them to the guest VM over vsock into `/run/secrets/` (tmpfs, never persisted to disk). Memory is zeroed after transmission.

Sequence: `aegisclaw secrets add <name>` → vault stores encrypted `.age` file → skill activation → `SecretProxy.Inject()` → vsock delivery → VM uses secrets in memory.

## Fit in the Broader System

Design implemented by `internal/vault` (secret storage + proxy) and `internal/sandbox` (nftables egress rules). Pairs with `docs/pr19-network-secrets-security-ux-plan.md`. The `SecretProxy` type in `internal/vault` is the runtime delivery mechanism.
