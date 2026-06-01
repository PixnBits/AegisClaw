# 14 - Store VM Persistence

## Goal
Implement the Store VM with git-backed persistence, skill registry, and tamper-evident audit log.

## Acceptance Criteria
- Store VM starts and maintains persistent state
- Skill registry works with skill.list / tool.list
- Audit log is append-only and tamper-evident
- Proposal and Court decision storage functional

## Relevant Specs
- `store-vm.md`
- `skill-discovery.md`
- `governance-court.md`

## Test
- Skills survive restart
- Audit log entries are verifiable