# `docs/threat-model.md` — Summary

## Purpose

Formal security threat model for AegisClaw, identifying assets, trust boundaries, threat actors, attack vectors, and mitigations. Provides the security rationale underlying the paranoid-by-design architecture choices.

## Key Contents (Inferred from architecture and PRD context)

### Assets
- User secrets (API keys, tokens) stored in the age-encrypted vault.
- Audit log integrity (append-only Merkle tree with Ed25519 signatures).
- Skill code authenticity (git-backed, signed before activation).
- Host system integrity (no skill or agent can escape to the host).
- LLM context privacy (no secrets in prompts, logs, or LLM context).

### Trust Boundaries
- Host daemon ↔ AegisHub microVM (vsock, VMM operations only)
- AegisHub ↔ all other microVMs (vsock, ACL-enforced)
- CLI ↔ daemon (Unix socket, local unprivileged user)
- Skill VMs ↔ external network (default-deny nftables, FQDN allowlist only)

### Threat Actors
- Malicious skill code (prompt injection → code execution in sandbox only, no host escape)
- Compromised LLM output (tool call injection — mitigated by AegisHub ACL and structured output enforcement)
- Supply chain attacks (mitigated by SBOM, SCA gates, Alpine signature verification)
- Physical access (age-encrypted vault, keys derived from Ed25519 keypair)

### Key Mitigations
- `cap-drop ALL` on every microVM
- Read-only rootfs; writable `/workspace` only
- Firecracker KVM isolation as hard dependency
- Mandatory Governance Court review (no bypass)
- Append-only audit log with cryptographic verification

## Fit in the Broader System

Referenced by `docs/PRD.md` (security principles section). Informs design decisions in `internal/sandbox`, `internal/vault`, `internal/audit`, `internal/ipc`, and `internal/builder/securitygate`.
