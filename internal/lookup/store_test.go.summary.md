# store_test.go

## Purpose
Unit and integration tests for the `Store` type in `internal/lookup`. Validates the full lifecycle of the vector store: creating a new store, indexing multiple tools, performing semantic lookups, and verifying result relevance. Tests confirm that the store returns the most relevant tools for a given query when a sufficient number of tools have been indexed, and that `Count` accurately reflects the number of indexed entries.

## Key Types and Functions
- `TestNewStore`: verifies store creation and directory initialisation
- `TestIndexTool`: indexes individual tools and checks no errors are returned
- `TestLookupTools`: indexes several distinct tools and asserts that a semantic query returns the expected top result
- `TestCount`: verifies that `Count` returns the correct number after a series of `IndexTool` calls
- Uses `t.TempDir()` for isolated test directories

## Role in the System
Provides confidence that the semantic tool-discovery layer works correctly end-to-end. Since the lookup store is critical for routing agent tasks to the correct skill tools, these tests guard against regressions in indexing, search ranking, and persistence.

## Dependencies
- `testing`: standard Go test framework
- `context`: for passing contexts to store methods
- `internal/lookup`: package under test
