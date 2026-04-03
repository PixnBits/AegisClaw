# Memory Store – Tiered Persistent Memory System

**Document Status**: Draft v0.1  
**Last Updated**: 2026-04-02  
**Owner**: Project Lead (Governance Court review required before implementation or changes)  
**Related Documents**:  
- `docs/agentic-evolution.md` (Hierarchical architecture, async primitives, human approvals)  
- `docs/agent-prompts.md` (Orchestrator prompt with memory-first rule and few-shot examples)  
- `docs/event-bus-and-async.md` (Signal/timer wakeup with automatic memory injection)  
- `docs/PRD.md` (Paranoid-by-design, auditability, isolation, GDPR/CCPA foundations)  
- `docs/architecture.md` (Firecracker microVMs, AegisHub proxy injection, Merkle audit tree)  

This document defines the **Tiered Memory Store** that gives AegisClaw agents long-term persistence, continuity across async wakeups, and controlled compaction over time while maintaining strict security, encryption, and auditability.

## Goals

- Enable the Orchestrator and Workers to remember past decisions, tasks, and context across sessions and long-running async workflows.
- Support your three core user stories (background research, OSS issue→PR, recurring summaries) with reliable state.
- Provide automatic, configurable compaction to keep storage usage reasonable on end-user hardware (32 GB RAM machines).
- Enforce privacy controls (GDPR/CCPA right-to-forget) from day one.
- Ensure every memory operation is auditable and tamper-evident via the Merkle tree.
- Prevent memory poisoning or drift through design and optional critic mechanisms.

## Architecture & Components

The Memory Store is a **host-level service** (part of the main AegisClaw daemon) with proxy access injected into agent microVMs via AegisHub. It is never directly accessible from inside VMs.

Core pieces:

1. **Storage Backend**
   - Primary: Encrypted SQLite database (per-user, with WAL mode for concurrency).
   - Secondary: Local vector store for semantic search (Chroma or lightweight LanceDB / SQLite + embeddings).
   - Embeddings generated via Ollama (`nomic-embed-text` or `snowflake-arctic-embed` — runs efficiently on CPU).

2. **Encryption Layer**
   - Per-user AES-256-GCM keys derived from the user’s system keyring (e.g., macOS Keychain, Linux secret-service, Windows Credential Manager).
   - All values encrypted at rest; keys never leave the host daemon.

3. **Access Proxy**
   - Thin proxy service inside each agent VM (injected at startup).
   - Enforces ACLs: read/write only for the current task’s context or with explicit user approval.
   - All calls logged to Merkle audit tree before execution.

4. **Compaction Daemon**
   - Background cron job (runs daily at low priority).
   - Automatically compacts memories according to their TTL tier.

## Data Model

```json
{
  "memory_id": "uuid",
  "task_id": "string (optional, links to async task)",
  "key": "string (human-readable or semantic)",
  "value": "encrypted string or json",
  "embedding": "float vector (for semantic search)",
  "tags": ["array of strings"],
  "security_level": "low | medium | high",
  "ttl_tier": "90d | 180d | 365d | 2yr | forever",
  "created_at": "ISO8601",
  "last_compacted_at": "ISO8601 or null",
  "version": "int (for soft updates)",
  "deleted": "bool (soft delete for recovery)"
}
```

- **Key**: Can be exact-match or used for semantic search.
- **Value**: Structured JSON preferred for facts/state; free text for episodic memories.
- **Tags**: Useful for filtering (e.g., ["research", "oss-pr", "recurring"]).
- **Security Level**: Influences redaction and approval requirements.

## Tiered Retention & Compaction

Exactly as specified:

| Tier       | Retention Period       | Fidelity                          | Size Reduction Target |
|------------|------------------------|-----------------------------------|-----------------------|
| 90d        | 0 – 90 days            | Full (raw traces, full details)   | None                  |
| 180d       | 91 – 180 days          | Medium (key facts + decisions)    | ~70%                  |
| 365d       | 181 – 365 days         | Heavy (bullet points only)        | ~85%                  |
| 2yr        | 366 days – 2 years     | Ultra-summary (1–2 paragraphs)    | ~95%                  |
| forever    | Indefinite             | Archive (1 sentence + metadata)   | ~98%                  |

**Compaction Process**:
- Background daemon scans daily.
- For each memory past its tier threshold → generate compacted version using a small local summarizer model (or Orchestrator if lightweight enough).
- Original full version kept for 30 days after compaction for recovery.
- User can manually trigger `compact_memory(task_id, target_tier)` via agent or dashboard.

**Default Policy**:
- New memories start at "90d" unless explicitly set to "forever".
- Configurable globally via `aegisclaw memory policy set`.

## Tools Exposed to Agents

Discovered via `search_tools`. Orchestrator and authorized Workers can use:

- `store_memory(key: str, value: str|json, tags: list[str], ttl_tier: str, security_level: str) → memory_id`
- `retrieve_memory(query: str, k: int = 5, filters: json = null) → list[MemorySummary]`
  - Supports semantic search + exact key match + tag/time filters.
- `compact_memory(task_id: str, target_tier: str) → bool`
- `delete_memory(query: str) → int (number deleted)` — for GDPR/CCPA
- `list_memories(filter: json) → list` — mainly for dashboard

**Strict Rules in Prompts**:
- Always call `retrieve_memory` early in ReAct reasoning.
- Never store raw secrets or high-sensitivity PII (redact or use `manage_secrets`).
- On wakeup from signal/timer: memory context is auto-injected by the Event Bus dispatcher.

## Privacy & Compliance

- **GDPR / CCPA**:
  - `delete_memory` removes all matching entries across tiers (including compacted versions).
  - Audit log records every deletion with reason (e.g., "user requested right to be forgotten").
  - Consent logged on every `store_memory` with non-default tier or sensitive tags.

- **PII Handling**:
  - Optional lightweight local redaction step before storage (regex + small classifier model).
  - High-security-level memories require `request_human_approval` before storage.

- **SOC 2 Path**:
  - All encryption key operations logged.
  - Immutable audit trail for every read/write/compaction.

## Security Considerations

- **Memory Poisoning Mitigation**:
  - Optional “Memory Critic” (tiny 3B model) that periodically scores memories for consistency and flags anomalies.
  - Versioning + soft-delete allows rollback of bad compactions.

- **Access Control**:
  - Proxy rejects cross-task memory writes unless explicitly allowed.
  - Dashboard allows user to review/delete any memory.

- **Resource Limits**:
  - Hard cap on total memory store size (default 2–4 GB per user, configurable).
  - Vector index pruned during compaction.

- **Integration with Audit**:
  - Every `store_memory`, `retrieve_memory`, and compaction is appended to the Merkle tree with before/after hashes where appropriate.

## Integration with Other Systems

- **Event Bus / Wakeup**: On signal fire, the dispatcher automatically calls `retrieve_memory` for the task_id and injects a compact summary as the first Observation.
- **Hierarchical Agents**: Workers can read (but not write) shared task memory; only Orchestrator writes persistent state.
- **Dashboard**: Full semantic search UI, tier visualization, manual compaction/delete controls, export.
- **Governance Court**: Any proposal that significantly changes memory behavior (new tiers, critic logic, encryption scheme) must be reviewed.

## Open Questions & Trade-offs

- Embedding model choice: nomic vs. arctic vs. larger model for better quality (trade-off speed vs. accuracy on 3080/4080).
- Compaction trigger: Pure time-based vs. usage-based (when store size exceeds threshold)?
- Critic frequency: Daily? On every 10th wakeup? User-configurable?
- Multi-user support: Separate encrypted stores per user vs. shared with strong isolation.

## Decision Log

- **2026-04-02**: Chose hybrid SQLite + vector store for balance of exact match, semantic search, and simplicity on end-user hardware.
- **2026-04-02**: Adopted user-specified 5-tier compaction model with automatic daily background processing.
- **2026-04-02**: Encryption keys managed via system keyring; no keys stored in database.
- **2026-04-02**: Memory-first rule enforced in Orchestrator prompt; auto-injection on wakeup.

**Next Actions**:
- Select and integrate embedding model into Ollama setup.
- Implement encrypted SQLite schema + proxy.
- Build compaction daemon and critic prototype.
- Add Memory Vault section to web dashboard.
- Submit this spec for Governance Court review.

Any modifications to memory tiers, storage backend, or access rules must follow the full proposal → Governance Court process.
