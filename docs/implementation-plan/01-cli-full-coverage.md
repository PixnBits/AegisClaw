# 01 - CLI Full Coverage

**Goal**: Implement all missing CLI verbs and subcommands referenced in `docs/specs/cli.md` and `docs/specs/additional-requirements-and-gaps.md` so that the `aegisclaw` (or `aegis`) binary exposes the full intended interface.

## Current State (from gaps analysis)
- Many verbs are stubbed or missing end-to-end: `restart`, `team *`, `skills status`, `court decisions show`, session/task status and control, `autonomy grant/revoke/reset`, `audit verify`.
- Secrets lifecycle (`aegis secrets set/list/remove`) is incomplete.

## Tasks

1. **Audit existing CLI code** (`cmd/aegisclaw/*.go` or equivalent)
   - Map current commands vs. spec.
2. **Implement missing top-level commands**:
   - `aegisclaw restart [component]`
   - `aegisclaw team list|create|join|leave|status`
   - `aegisclaw skills status [name]`
   - `aegisclaw court decisions show [id]`
   - `aegisclaw session|task status|cancel|pause`
   - `aegisclaw autonomy grant|revoke|reset <agent-id>`
   - `aegisclaw audit verify [hash|latest]`
3. **Complete secrets subcommand**:
   - `aegisclaw secrets set <key> [--stdin|--file]` (interactive or piped)
   - `aegisclaw secrets list`
   - `aegisclaw secrets remove <key>`
   - Wire to Vault implementation (see step 05)
4. **Add shell completion** and `--help` improvements for all new verbs.
5. **Integration tests**: Add to `testdata/` or Playwright harness for each new verb (non-destructive where possible).

## Acceptance Criteria
- All listed verbs execute without "unknown command" errors.
- Help text matches `docs/specs/cli.md`.
- Secrets commands integrate with encrypted storage (no plaintext in args/history).
- Automated tests cover happy path + error cases for each.
- No regression on existing commands (`start`, `status`, `eval`, etc.).

**Dependencies**: None (can run in parallel with early steps).
**Estimated effort**: 1-2 days focused work.

**Owner**: TBD (AI agent or developer)
**Status**: Ready to start