# components.go

## Purpose
Provides reusable, composable TUI primitives that are shared across AegisClaw's multiple dashboard views. Each component is a self-contained bubbletea model with its own `Update`/`View` methods, following the Elm architecture pattern. The primitives are kept generic so they can be embedded in any model without modification.

## Key Types and Functions
- `KeyMap` / `DefaultKeyMap`: vi-style keybindings — up/k, down/j, esc=back, a=approve, x=reject, r=refresh, `/`=search, pgup/pgdn; uses `charmbracelet/bubbles/key`
- `Table`: scrollable data table with focused/blurred visual states and keyboard navigation; `NewTable(cols []string, height int)`, `SetRows([][]string)`, `SelectedRow() []string`, `Focus()`/`Blur()`/`Focused()`, `Update(tea.Msg)`, `View() string`
- `SpinnerModel`: animated loading spinner with a status message; `NewSpinner(msg string)`, `Done bool`, `View() string`
- `Modal`: double-border overlay for confirmation dialogs; `NewModal(title string)`, `Show(content string)`, `Hide()`, `View() string`
- `DiffViewer`: syntax-coloured unified diff display with scroll; `NewDiffViewer(height int)`, `SetContent(diff string)`, `ScrollDown()`/`ScrollUp()`, `View() string`
- `Truncate(s string, max int) string`: truncates with `".."` suffix at max runes
- `HelpView(bindings ...string) string`: formats a help bar from key/description pairs

## Role in the System
Embedded in the court dashboard, audit explorer, status dashboard, and chat views. Consistent keyboard behaviour and visual primitives across all views are achieved by sharing these components rather than reimplementing them per-view.

## Dependencies
- `github.com/charmbracelet/bubbletea`: Elm-architecture message passing
- `github.com/charmbracelet/lipgloss`: styling
- `github.com/charmbracelet/bubbles`: spinner and key binding helpers
