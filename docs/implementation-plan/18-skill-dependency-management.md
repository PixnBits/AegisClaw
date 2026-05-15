# 18 - Skill Dependency Management

**Goal**: Implement proper dependency tracking, versioning, secure composition, and update mechanisms for skills.

## Why This Matters
From `docs/specs/additional-requirements-and-gaps.md`:
- Skill dependency management is listed as a remaining open question.
- Skills often depend on other skills, libraries, or external services — we need safe, auditable ways to manage this.

## Tasks

1. **Define dependency model**
   - Create `SkillDependency` type with version constraints, scopes, and trust levels
2. **Dependency resolution engine**
   - Resolve and validate dependencies at proposal/build time
   - Detect circular dependencies and version conflicts
3. **Secure composition**
   - Enforce scope boundaries between dependent skills
   - Audit all dependency usage
4. **Update mechanism**
   - Safe update paths with court approval for breaking changes
5. **Tests**
   - Dependency resolution tests
   - Conflict and circular dependency detection

## Acceptance Criteria
- Skills can declare and resolve dependencies safely
- Version conflicts and circular dependencies are detected
- Full audit trail for dependency usage

**Dependencies**: Skill registry + builder pipeline
**Estimated effort**: 2–3 days

**Owner**: TBD
**Status**: Ready to start