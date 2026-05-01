# status_dashboard_test.go

## Purpose
Tests for `StatusDashboardModel` covering status data rendering, tab switching between sandboxes and skills panels, sandbox start/stop action guards, and nil callback safety. Mock callbacks return pre-defined sandbox and skill rows without requiring a live runtime.

## Key Types and Functions
- `TestStatusDashboard_Init`: verifies `Init` triggers the `LoadStatus` callback
- `TestStatusDashboard_RenderSandboxes`: loads sandbox rows; verifies sandbox names, statuses, and resource info appear in `View`
- `TestStatusDashboard_RenderSkills`: loads skill rows; verifies skill names and states appear in `View`
- `TestStatusDashboard_TabSwitch`: presses `Tab` and verifies the active panel switches between Sandboxes and Skills
- `TestStatusDashboard_StopRunning`: selects a running sandbox and presses `s`; verifies `StopSandbox` callback is invoked
- `TestStatusDashboard_NoStopStopped`: selects a stopped sandbox and presses `s`; verifies `StopSandbox` is NOT called (action guard)
- `TestStatusDashboard_StartStopped`: selects a stopped sandbox and presses enter; verifies `StartSandbox` is called
- `TestStatusDashboard_NoStartRunning`: selects a running sandbox and presses enter; verifies `StartSandbox` is NOT called
- `TestStatusDashboard_NilCallbacks`: sends events with nil callbacks; verifies no panics

## Role in the System
Ensures the operator status view correctly reflects system state and guards against nonsensical lifecycle operations, preventing accidental double-starts or stops.

## Dependencies
- `testing`, `github.com/charmbracelet/bubbletea`
- `internal/tui`: `StatusDashboardModel`, `SandboxRow`, `SkillRow`
