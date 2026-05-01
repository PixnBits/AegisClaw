# Package: tui

## Overview
The `tui` package implements AegisClaw's terminal user interface using the charmbracelet/bubbletea Elm-architecture framework. It provides five major views — chat, Governance Court dashboard, audit chain explorer, system status dashboard, and shared component primitives — plus a centralised style system. All views use composable bubbletea models and communicate via callback functions, keeping the TUI layer decoupled from business logic.

## Files
- `styles.go`: Catppuccin-inspired colour palette and semantic style helpers (`RiskStyle`, `StatusStyle`, `SandboxStateStyle`)
- `components.go`: Reusable primitives — `Table`, `SpinnerModel`, `Modal`, `DiffViewer`, `Truncate`, `HelpView`, `DefaultKeyMap`
- `chat.go`: ReAct agent chat interface with input history, tool queuing, safe mode, and proposal status polling
- `court_dashboard.go`: Governance Court voting dashboard — proposal list, detail, diff, and vote confirmation flow
- `audit_explorer.go`: Audit chain explorer with search, chain verification, and rollback confirmation
- `status_dashboard.go`: System status dashboard showing sandbox and skill states with start/stop actions
- `audit_explorer_test.go`: Tests for audit explorer view transitions and keyboard handling
- `chat_test.go`: Tests for chat model message flow, tool queuing, safe mode, and input history
- `components_test.go`: Tests for all shared primitive components
- `court_dashboard_test.go`: Tests for Governance Court vote flow and view navigation
- `status_dashboard_test.go`: Tests for status dashboard rendering and action guards

## Key Abstractions
- Each TUI model satisfies `tea.Model` (`Init`/`Update`/`View`)
- Callback pattern: all data I/O flows through injected callback functions, keeping models testable in isolation
- `DefaultKeyMap`: consistent vi-style keybindings across all views
- `styles.go`: single source of truth for all colours and typographic styles

## System Role
The `tui` package is the user-facing shell of the AegisClaw daemon. It is assembled and started by the `cmd/aegisclaw` main package, connecting callbacks to the actual daemon services (`internal/proposal`, `internal/sandbox`, `internal/sessions`, `internal/kernel`).

## Dependencies
- `github.com/charmbracelet/bubbletea`: Elm-architecture runtime
- `github.com/charmbracelet/lipgloss`: terminal styling
- `github.com/charmbracelet/bubbles`: spinner and key binding utilities
- `time`: proposal status poll tick
