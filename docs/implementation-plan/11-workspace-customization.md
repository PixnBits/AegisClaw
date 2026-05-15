# 11 - Workspace Customization Loading

**Goal**: Implement loading of user-defined customization files from `~/.aegis/workspace/` (`AGENTS.md`, `SOUL.md`, `TOOLS.md`, `SKILL.md`) into every Agent Runtime on startup.

## Requirements from Specs
From `docs/specs/additional-requirements-and-gaps.md`:
- Support loading user-defined context files from `~/.aegis/workspace/`
- `AGENTS.md` — custom agent personas
- `SOUL.md` — system soul / values
- `TOOLS.md` — tool descriptions
- `SKILL.md` — skill templates
- This enables strong personalization

## Tasks

1. **Define workspace file schema**
   - Create structs for each file type in `internal/workspace/`
2. **Implement loader**
   - On agent/runtime startup, read files from `~/.aegis/workspace/`
   - Merge with defaults (user files take precedence)
   - Inject into agent prompt/system context
3. **Security considerations**
   - Validate file size and content (prevent prompt injection attacks)
   - Sandbox the loading process
4. **Tests**
   - Unit tests for loading + merging
   - Integration test: custom `AGENTS.md` changes agent behavior

## Acceptance Criteria
- All four files are loaded when present
- Content is correctly injected into agent context
- Changes take effect on next agent start
- Full alignment with `docs/specs/agent-customization.md`

**Dependencies**: Follows directory layout (02) and runtime work
**Estimated effort**: 1.5–2 days

**Owner**: TBD
**Status**: Ready to start