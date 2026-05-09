# Additional Requirements & Identified Gaps

## 1. Skill & Tool Discovery / Lookup

**Requirement**: Agents must be able to query the available skills and tools at runtime.

- New skill/tool: `list_skills` / `list_tools` (or combined `get_capabilities`)
- Should return: name, description, required scopes/permissions, current status, version
- Must be fast and available in every Agent Runtime VM via AegisHub

## 2. Secrets Management

**Requirement**: All secret handling must be done through the CLI (never via chat or autonomous agent action).

- `aegis secrets set <name>` → interactive prompt or `--stdin`
- `aegis secrets list` (shows names only)
- `aegis secrets remove <name>`
- Secrets stored encrypted and injected only at execution time by Network Boundary VM

## 3. Upgrade Process

Current accepted process for v2:
1. `aegis safe-mode enable` (or `aegis stop`)
2. `git pull`
3. `make build` / `make install`
4. `aegis start`

## 4. Open Questions (No Answers Yet)

- Global configuration system and where defaults live
- Resource quotas and host protection mechanisms
- Skill dependency management inside Builder VM
- Backup / restore strategy for Store VM
- Standardized error taxonomy and user-facing error messages
- Full threat model documentation
- Audit log retention and pruning policy
- Packaging and distribution story beyond dev mode

## Next Actions
- Create dedicated specs for Secrets Management, Configuration, and Skill Discovery

**Last Updated:** May 08, 2026