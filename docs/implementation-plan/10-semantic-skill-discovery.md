# 10 - Semantic Skill & Tool Discovery

**Goal**: Implement `list_skills()` and `list_tools()` (with optional semantic/vector search) inside the Agent Runtime so agents can dynamically discover available capabilities at runtime.

## Requirements from Specs
- `docs/specs/additional-requirements-and-gaps.md` explicitly calls for:
  - Dedicated tool: `list_skills()`, `list_tools()`, or `get_capabilities()`
  - Returns: name, description, required scopes, version, status
  - Must support semantic search (vector embeddings)
  - Must be fast and available in every Agent Runtime VM

## Tasks

1. **Define capability schema**
   - Create `internal/skills/types.go` with `SkillInfo` and `ToolInfo` structs
2. **Implement discovery backend**
   - Store skill/tool metadata in the Store VM (or in-memory registry for speed)
   - Add vector embedding support (e.g., via Ollama or local model) for semantic search
3. **Expose via Agent Runtime**
   - Add `list_skills(query string, limit int)` and `list_tools(query string, limit int)` to the tool registry
   - Support both exact match and semantic similarity
4. **Security & scoping**
   - Respect agent skill scopes and governance approvals
   - Never leak unapproved or restricted capabilities
5. **Tests**
   - Unit tests for exact + semantic search
   - Integration test: agent calls `list_skills("code review")` → relevant skills returned

## Acceptance Criteria
- Agents can call `list_skills()` and `list_tools()` successfully
- Semantic search returns relevant results (tested with real embeddings)
- Performance is fast (< 100ms typical)
- Full alignment with `docs/specs/semantic-tool-discovery.md`

**Dependencies**: Follows core runtime and skill registry work
**Estimated effort**: 2–3 days

**Owner**: TBD
**Status**: Ready to start