# audit_explorer_test.go

## Purpose
Tests for `AuditExplorerModel` covering its full bubbletea lifecycle: `Init`, `Update`, and `View`. Tests verify view transitions triggered by keyboard events, data loading via mock callbacks, search filtering, chain verification display, and rollback confirmation flow. Nil callback safety is explicitly tested to ensure the model degrades gracefully when optional callbacks are not provided.

## Key Types and Functions
- `TestAuditExplorerInit`: verifies `Init` returns a command (the `LoadEntries` trigger)
- `TestAuditExplorer_ViewList`: verifies the list view renders entry IDs and timestamps
- `TestAuditExplorer_Navigation`: sends `j`/`k` key messages and verifies the selected index changes
- `TestAuditExplorer_ViewDetail`: selects an entry and presses enter; verifies the detail view renders payload content
- `TestAuditExplorer_Search`: enters search mode, types a query, and verifies filtered results
- `TestAuditExplorer_Verify`: triggers chain verification and verifies the verify view renders correctly
- `TestAuditExplorer_Rollback`: navigates to rollback confirm view and confirms; verifies `RollbackEntry` callback is invoked
- `TestAuditExplorer_NilCallbacks`: creates a model with nil callbacks and sends various key presses; verifies no panics

## Role in the System
Guards the audit explorer from regressions in view transitions and keyboard handling. Since audit log integrity is a core security feature, the explorer must always render correctly and never panic.

## Dependencies
- `testing`, `github.com/charmbracelet/bubbletea`
- `internal/tui`: `AuditExplorerModel`, `AuditEntry`
