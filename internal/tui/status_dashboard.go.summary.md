# status_dashboard.go

## Purpose
Implements the system status dashboard — a two-panel TUI view showing the current state of all sandboxes and all deployed skills. The dashboard uses Tab key to switch between the sandboxes panel and the skills panel. Action buttons (start/stop) are guarded by smart checks that prevent nonsensical operations (e.g., starting an already-running sandbox).

## Key Types and Functions
- `StatusDashboardModel`: bubbletea `Model`; tracks active tab, loaded sandbox rows, skill rows, status info, and selected row indices
- `SandboxRow`: ID, Name, Status, VCPUs, MemoryMB, HostIP, StartedAt
- `SkillRow`: Name, SandboxID, State, Version
- `StatusInfo`: kernel public key fingerprint, audit log entry count, chain head hash, skill registry root hash
- Tab switching: `Tab` key alternates between Sandboxes and Skills panels
- Action guards: `s` to stop a running sandbox, `enter` to start a stopped sandbox; no-op if action is inappropriate
- `Init() tea.Cmd`: triggers `LoadStatus` callback on startup
- Callbacks: `LoadStatus`, `StopSandbox`, `StartSandbox`

## Role in the System
The status dashboard is the operator's at-a-glance health view. It surfaces which VMs are running, what skills are deployed, and key audit chain integrity indicators. Accessible from the daemon TUI's top-level navigation.

## Dependencies
- `github.com/charmbracelet/bubbletea`: model lifecycle
- `internal/tui`: `Table`, shared styles, `DefaultKeyMap`
