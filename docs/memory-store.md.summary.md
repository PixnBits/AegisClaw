# `docs/memory-store.md` — Summary

## Purpose

Specifies the **Tiered Memory Store** architecture that gives AegisClaw agents long-term persistence, continuity across async wakeups, and controlled compaction — while maintaining encryption, auditability, and GDPR/CCPA privacy foundations.

## Key Contents

### Goals
- Enable Orchestrator and Workers to remember past decisions, tasks, and context across sessions.
- Support the three core user stories (background research, OSS issue → PR, recurring summaries).
- Automatic configurable compaction to stay within end-user hardware limits.
- Privacy controls: GDPR/CCPA right-to-forget from day one.
- Every memory operation is auditable and tamper-evident via the Merkle tree.

### Architecture Components
1. **Storage Backend**: Primary — age-encrypted JSONL (implemented as `internal/memory`); secondary — local vector store for semantic search.
2. **Encryption Layer**: Per-user keys derived from the daemon's Ed25519 key via HKDF-SHA256; keys never leave host daemon.
3. **Access Proxy**: Thin proxy inside each agent VM (injected at startup); VMs never have direct DB access.
4. **Tiered TTLs**: Working memory (session-scoped), episodic (days/weeks), semantic (long-term with compaction).
5. **PII Scrubber**: 7 regex rules for email, phone, SSN, IPv4, JWT, AWS key, generic API key.

### Tools
`store_memory`, `retrieve_memory`, `forget_memory`, `list_memories` — all routed via AegisHub.

## Fit in the Broader System

Implemented in `internal/memory`. Integrated with `internal/vault` (age encryption), `internal/audit` (Merkle logging), and `internal/eventbus` (memory injection at wakeup). Config key: `memory.pii_redaction`.
