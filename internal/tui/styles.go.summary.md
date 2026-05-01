# styles.go

## Purpose
Defines the shared colour palette and `lipgloss` style variables used across all AegisClaw TUI components. Centralising styles here ensures visual consistency — every component uses the same Catppuccin-inspired colours and typographic conventions. It also provides semantic style helpers (`RiskStyle`, `StatusStyle`, `SandboxStateStyle`) that map domain values to their appropriate visual treatment.

## Key Types and Functions
- Colour palette constants: `mauve`, `blue`, `green`, `red`, `yellow`, `teal`, `surface`, `overlay`, `base`, `text`, `subtext`, `crust`
- `RiskStyle(level string) lipgloss.Style`: returns coloured style for `low` (green), `medium` (yellow), `high` (red), `critical` (mauve/bold)
- `StatusStyle(status string) lipgloss.Style`: returns style for `approved`, `rejected`, `in_review`, `draft`, and other proposal statuses
- `SandboxStateStyle(state string) lipgloss.Style`: returns style for `running`, `stopped`, `error` sandbox states
- Shared named styles: `TitleStyle`, `SubtitleStyle`, `HeaderStyle`, `SelectedStyle`, `MutedStyle`, `BorderStyle`, `HelpStyle`, `ErrorStyle`, `BadgeStyle`
- Status styles: `StatusApproved`, `StatusRejected`, `StatusPending`, `StatusDraft`

## Role in the System
Imported by every TUI model file (`chat.go`, `court_dashboard.go`, `audit_explorer.go`, `status_dashboard.go`, `components.go`). Changes to the palette or semantic styles instantly propagate to the entire TUI, making this file the single source of truth for AegisClaw's visual identity.

## Dependencies
- `github.com/charmbracelet/lipgloss`: style and colour primitives
