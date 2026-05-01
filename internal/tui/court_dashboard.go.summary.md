# court_dashboard.go

## Purpose
Implements the Governance Court review dashboard — the TUI view where personas cast votes on skill deployment proposals. The dashboard has four sub-views: a proposal list, a proposal detail view, a diff viewer for spec changes, and a vote confirmation modal. Navigation between views is driven by keyboard shortcuts from `DefaultKeyMap`.

## Key Types and Functions
- `CourtDashboardModel`: bubbletea `Model`; tracks current view state, selected proposal, loaded sessions, and vote in progress
- `CourtProposal`: display-oriented proposal record (ID, Title, Status, Risk, Author, Round, ReviewCount)
- `CourtSession`: active court session metadata
- `CourtReview`: individual persona review record with verdict and risk score
- Views: `viewList` → `viewDetail` → `viewDiff` or `viewVoteConfirm` → back to list
- Vote flow: select proposal → press `a`/`x` → confirmation modal → `CastVote` callback invoked
- `Init() tea.Cmd`: triggers `LoadProposals` callback
- `Update(tea.Msg)`: handles list navigation, view transitions, modal confirmation, and data-load responses
- Callbacks: `LoadProposals`, `LoadSessions`, `LoadDiff`, `CastVote`

## Role in the System
The primary interface for human-in-the-loop governance review. When a new skill proposal enters the `in_review` state, operators use this dashboard to read the spec, view the diff, and cast approve/reject votes. The vote is committed back to the proposal store via `CastVote`.

## Dependencies
- `github.com/charmbracelet/bubbletea`: model lifecycle
- `internal/tui`: `Table`, `Modal`, `DiffViewer`, shared styles
