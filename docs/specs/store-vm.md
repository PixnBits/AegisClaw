# Store VM Specification

**Status:** Draft  
**Last Updated:** June 2026 (Phase 2 timer/grant responsibilities added)

## Purpose

The Store VM serves as both the **persistent data store** and the **trusted git remote** for all skills.

## For Implementers

The Store VM is the single source of truth and enforcement point for proposals, git state, PRs, audit, and skill registry. When implementing or extending:
- All mutations to git, PRs, proposals, or grants must go through Store commands.
- Enforce branch protection and Court-only final merge.
- Maintain the tamper-evident Merkle audit log on every relevant action.
- Integrate permission grants and visibility (permissions-model.md) for proposal, Court, and Builder-related capabilities.
- Expose the documented command surfaces cleanly over the signed vsock API.

## Responsibility Boundaries

**Store VM owns:**
- All Change Proposals and their governance history
- The tamper-evident Merkle tree audit log
- The skill registry and `network-access.yaml` declarations
- Long-term memory backups
- All skill git repositories (acts as the remote)
- Pull Request state and review records
- Persistent timers, autonomy grants, and background work expiration (Phase 2)
- Scheduled task state and reconciliation

**Store VM does NOT own:**
- Short-term conversation context
- Active semantic memory retrieval

These belong to the Memory VM.

## Note on Architecture

The Store VM acts as the central, trusted authority for both structured data and git operations. Builder VMs clone from and push to the Store VM, but cannot directly manipulate repositories on disk.

## Public API Commands

### Proposal Management
- `proposal.create`
- `proposal.get`
- `proposal.list`
- `proposal.update`

### Court & Review
- `court.submit_vote`
- `court.get_reviews`

### Git Repository Operations
- `git.clone` — Clone a skill repository
- `git.push` — Push branches/commits from a Builder VM
- `pr.create` — Create a new Pull Request from a branch
- `pr.update` — Update PR metadata or status
- `pr.get` — Retrieve PR details and review status

### Skill Registry
- `skill.register`
- `skill.get`
- `skill.list`

### Memory Backup
- `memory.store` — Called by Memory VM to back up long-term memories
- `memory.query` — Semantic search across backed-up memories

### Audit Log
- `audit.append`
- `audit.get_root`

### Timer & Grant Management (Phase 2)
The Store VM is the single source of truth for persistent timers, autonomy grants, and background work expiration.

### Chat Session Registry (Web Portal)
The Store VM owns durable web-portal chat session records (id, title, timestamps, message thread snapshots). The Web Portal forwards `sessions.*` bridge actions here; live chat turns flow through the agent chat system (`chat.message`, not the Host Daemon).

- `sessions.list` — Session summaries for the chat sidebar
- `sessions.create` — Create a new session
- `sessions.history` / `sessions.get` — Load a session including messages
- `sessions.save` — Persist title and/or messages after a turn

- `autonomy.grant` — Record a new autonomy grant (durable in grants.json)
- `grant.list` — List all current grants
- `grant.get` — Retrieve grant for a specific session
- `timer.schedule` — Schedule a durable timer
- `timer.cancel` — Cancel a scheduled timer
- `timer.list` — List active timers (rich metadata)
- `reconcile.expired_grants` — Authoritative expiration reconciliation (autonomy + background + general timers)
- The Store autonomously publishes `autonomy.expired`, `background.expired`, and `timer.fired` events via the Hub (see event-system.md)

## Architecture

- Dedicated Firecracker microVM
- SQLite + Litestream for data
- Hosts bare git repositories for all skills
- Strict, signed API over vsock

## Security Requirements

- All git operations must be authenticated and tied to a specific Builder VM + Proposal
- The Store VM enforces branch protection rules (no force push, no direct merges)
- Only the Store VM can perform the final merge after Court approval
- Pull Request reviews can only be submitted by Court personas
- Permission and visibility checks (permissions-model.md) gate sensitive proposal, Court, and grant operations.

## Test Requirements

- Modifying any past audit log entry must invalidate the Merkle root
- Builder VMs cannot bypass Court approval to merge code
- Only the Store VM can execute the final merge of an approved PR
- All git operations must be traceable to a specific proposal
- A malicious Builder VM must not be able to delete history or force-push
- Permission grants must be enforced before allowing proposal or Court-related commands.

