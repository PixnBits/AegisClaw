# `server_internal_test.go` — Internal (White-Box) Dashboard Tests

## Purpose
Uses `package dashboard` (same package) to test unexported helpers and template rendering functions that are not accessible from an external test package.

## Key Test Areas

| Area | What Is Tested |
|---|---|
| Template helper functions | `fmtTime`, `truncate`, `join`, `toJSON`, `substr`, `len` with nil/edge inputs |
| `countItems()` | Correctly counts items in `[]interface{}`, `map[string]interface{}`, and nil |
| `sandboxResourceTotals()` | Aggregates vCPUs, memory, and RSS across a slice of sandbox maps |
| `fetchRaw()` stub | Verifies that `APIClient.Call` errors are handled gracefully (returns nil, not panic) |
| Route registration | Ensures all expected paths are registered on the mux |

## Role in the System
Provides a white-box safety net for the dashboard helper layer. Because dashboard templates are rendered inline (no `.html` files on disk), these tests verify that data transformations applied before template execution are correct.

## Notable Dependencies
- Package under test: `dashboard`
- Standard library (`net/http/httptest`, `testing`)
