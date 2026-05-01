# court_dashboard_test.go

## Purpose
Tests for `CourtDashboardModel` covering proposal list rendering, view transitions, vote flow, and nil callback safety. Mock callbacks simulate data loading and vote casting without requiring a real proposal store or governance court session.

## Key Types and Functions
- `TestCourtDashboard_Init`: verifies `Init` triggers the `LoadProposals` callback
- `TestCourtDashboard_ListRender`: loads proposals via mock callback; verifies proposal titles, statuses, and risk levels appear in `View`
- `TestCourtDashboard_SelectProposal`: selects a proposal and presses enter; verifies the detail view renders
- `TestCourtDashboard_OpenDiff`: navigates to detail view and presses `d`; verifies diff view is shown with loaded diff content
- `TestCourtDashboard_ApproveFlow`: selects a proposal, presses `a`; verifies vote confirm modal appears with approval text; confirms with enter; verifies `CastVote` is called with `"approve"`
- `TestCourtDashboard_RejectFlow`: same as approve but with `x`; verifies `CastVote` called with `"reject"`
- `TestCourtDashboard_BackNavigation`: verifies `esc` returns to the previous view at each step
- `TestCourtDashboard_NilCallbacks`: sends events with all callbacks nil; verifies no panics

## Role in the System
Ensures the governance voting workflow is correct and that the dashboard never panics on nil callbacks. The court dashboard is the only interface for casting human votes on proposals, making its correctness critical to the governance process.

## Dependencies
- `testing`, `github.com/charmbracelet/bubbletea`
- `internal/tui`: `CourtDashboardModel`, `CourtProposal`
