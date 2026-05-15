# 09 - Missing CLI Verbs Implementation

**Goal**: Implement all remaining CLI verbs referenced in `docs/specs/cli.md` and `docs/specs/additional-requirements-and-gaps.md` so the `aegisclaw` binary has complete, production-ready coverage.

## Current State (from deep code analysis)
Many verbs are stubbed, partial, or missing end-to-end:
- `team *` (list/create/join/leave/status)
- `autonomy grant/revoke/reset <agent-id>`
- `court decisions show [id]`
- `skills status [name]`
- Full component `restart`
- Some secrets lifecycle edge cases

## Tasks

1. **Audit current CLI surface** (`cmd/aegisclaw/*.go` and `root.go`)
   - Map every command vs. spec requirements
2. **Implement missing top-level commands**:
   - `aegisclaw team list|create|join|leave|status`
   - `aegisclaw autonomy grant|revoke|reset <id>`
   - `aegisclaw court decisions show [id]`
   - `aegisclaw skills status [name]`
   - `aegisclaw restart [component]` (safe restart with state preservation)
3. **Complete secrets lifecycle** (already partial):
   - Ensure full `add/rotate/refresh/list` with proper error handling and audit logging
4. **Add shell completion + improved `--help`**
5. **Integration tests** for every new verb (non-destructive where possible)

## Acceptance Criteria
- All listed verbs execute without "unknown command" errors
- Help text matches `docs/specs/cli.md`
- Every new verb has automated tests
- No regression on existing commands

**Dependencies**: Follows 01 (basic CLI) and core security steps
**Estimated effort**: 2–3 days

**Owner**: TBD
**Status**: Ready to start