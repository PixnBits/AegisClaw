# audit_explorer.go

## Purpose
Implements the audit chain explorer TUI — a view for browsing, searching, and cryptographically verifying the kernel's append-only audit log. The explorer has five sub-views: list (all entries), detail (single entry), search (filter by ID/hash/payload), verify (chain integrity check), and rollback confirmation modal. It provides operators with full visibility into all system actions that have been committed to the audit log.

## Key Types and Functions
- `AuditExplorerModel`: bubbletea `Model`; tracks current view, selected entry, filter text, verification result, and rollback target
- `AuditEntry`: ID, PrevHash, Hash, Timestamp, Payload string, Valid bool (computed by chain verification)
- Views: `viewList` → `viewDetail`, `viewSearch`, `viewVerify`, `viewRollbackConfirm`
- `applyFilter(entries, query)`: filters entries whose ID, hash, or payload contains the query string
- `resolveIndex(filtered, original)`: maps a filtered-list index back to the original list index
- Messages: `AuditRefreshMsg`, `AuditDataMsg`, `AuditVerifyMsg`, `AuditRollbackMsg` (bubbletea commands)
- Callbacks: `LoadEntries`, `VerifyChain`, `RollbackEntry`

## Role in the System
Used by the `aegisclaw audit` CLI command and the TUI daemon mode. Operators use the explorer to verify that no audit entries have been tampered with (`VerifyChain`) and to initiate a rollback of a specific action (`RollbackEntry`) if a security incident is detected.

## Dependencies
- `github.com/charmbracelet/bubbletea`: model lifecycle and commands
- `internal/tui`: `Table`, `Modal`, shared styles and `DefaultKeyMap`
