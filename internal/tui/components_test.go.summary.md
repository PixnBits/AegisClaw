# components_test.go

## Purpose
Tests for the shared TUI primitives in `components.go`: `Table`, `SpinnerModel`, `Modal`, `DiffViewer`, `Truncate`, and `HelpView`. Each component is tested independently to verify correct rendering, keyboard navigation, focus management, and edge case handling.

## Key Types and Functions
- `TestTable_Navigation`: creates a table with rows, sends `j`/`k` key messages, and verifies `SelectedRow` returns the correct row
- `TestTable_EmptyRows`: verifies the table renders safely with no rows
- `TestTable_Focus`: verifies focused/blurred styling differs in `View` output
- `TestSpinner_View`: verifies spinner renders with provided message text
- `TestSpinner_Done`: marks spinner done and verifies `View` no longer shows spinner animation
- `TestModal_ShowHide`: calls `Show` then `Hide`; verifies visibility and content in `View`
- `TestDiffViewer_SetContent`: sets a unified diff string; verifies it appears in `View`
- `TestDiffViewer_Scroll`: calls `ScrollDown`/`ScrollUp` and verifies viewport position changes
- `TestTruncate`: verifies strings above max length are truncated with `".."` suffix and strings below are unchanged
- `TestHelpView`: verifies key/description pairs appear in the rendered help bar

## Role in the System
These component tests prevent regressions in the shared building blocks used by all TUI views. Because components are composed into every dashboard, a bug in `Table` or `Modal` would affect the entire TUI.

## Dependencies
- `testing`, `github.com/charmbracelet/bubbletea`
- `internal/tui`: `Table`, `SpinnerModel`, `Modal`, `DiffViewer`, `Truncate`, `HelpView`
