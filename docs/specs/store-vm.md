# Store VM Specification

**Status:** Draft  
**Last Updated:** May 2026

## Purpose

The Store VM serves as both the **persistent data store** and the **trusted git remote** for all skills.

## Responsibility Boundaries

**Store VM owns:**
- All Change Proposals and their governance history
- The tamper-evident Merkle tree audit log
- The skill registry and `network-access.yaml` declarations
- Long-term memory backups
- All skill git repositories (acts as the remote)
- Pull Request state and review records

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

## Test Requirements

- Modifying any past audit log entry must invalidate the Merkle root
- Builder VMs cannot bypass Court approval to merge code
- Only the Store VM can execute the final merge of an approved PR
- All git operations must be traceable to a specific proposal
- A malicious Builder VM must not be able to delete history or force-push
