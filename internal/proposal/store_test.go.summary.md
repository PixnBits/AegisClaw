# store_test.go

## Purpose
Integration tests for the git-backed proposal store. Tests exercise the complete lifecycle of proposals through the store: creation, updates, retrieval, listing, status filtering, and ID prefix resolution. Each test uses a temporary directory with a fresh git repository to ensure isolation.

## Key Types and Functions
- `TestCreate`: creates a proposal and verifies a branch is created and the proposal can be retrieved
- `TestUpdate`: creates then updates a proposal's status and verifies the new state is persisted
- `TestGet`: verifies correct proposal retrieval by full UUID
- `TestList`: creates multiple proposals and verifies all appear in `List` output
- `TestListByStatus`: creates proposals in different states and verifies status-based filtering
- `TestResolveID`: verifies that a 6-character UUID prefix resolves to the correct full ID, and that ambiguous/missing prefixes return appropriate errors
- `TestImport`: imports a proposal with a pre-assigned ID and verifies it is accessible

## Role in the System
Prevents regressions in the governance proposal persistence layer. Since every skill deployment decision is recorded in the proposal store, correctness of Create/Update/Get/List operations is critical to the integrity of the audit trail.

## Dependencies
- `testing`, `t.TempDir()`
- `internal/proposal`: package under test
- `github.com/go-git/go-git/v5`: transitively exercised through the store
