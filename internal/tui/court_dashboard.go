package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// courtView tracks which panel is active.
type courtView int

const (
	courtViewList courtView = iota
	courtViewDetail
	courtViewDiff
	courtViewVoteConfirm
)

// CourtProposal holds data for one row in the court dashboard.
type CourtProposal struct {
	ID       string
	Title    string
	Category string
	Status   string
	Risk     string
	Author   string
	Round    int
	Updated  time.Time
}

// CourtSession holds data about an active court session.
type CourtSession struct {
	SessionID  string
	ProposalID string
	State      string
	Round      int
	RiskScore  float64
	Verdict    string
	Personas   []string
	Reviews    []CourtReview
}

// CourtReview holds per-persona review data.
type CourtReview struct {
	Persona   string
	Verdict   string
	RiskScore float64
	Comments  string
	Evidence  []string
	Round     int
}

// CourtDashboardModel is the bubbletea Model for the court review dashboard.
type CourtDashboardModel struct {
	proposals  []CourtProposal
	sessions   []CourtSession
	table      Table
	diffViewer DiffViewer
	modal      Modal
	keys       KeyMap
	view       courtView
	selected        int
	width           int
	height          int
	err             error
	voteAction      string
	prevView        courtView
	voteProposalIdx int

	// Callbacks for data loading
	LoadProposals func() ([]CourtProposal, error)
	LoadSessions  func() ([]CourtSession, error)
	LoadDiff      func(proposalID string) (string, error)
	CastVote      func(proposalID string, approve bool, reason string) error
}

// CourtRefreshMsg signals that proposal/session data should be reloaded.
type CourtRefreshMsg struct{}

// CourtDataMsg carries refreshed proposal and session data.
type CourtDataMsg struct {
	Proposals []CourtProposal
	Sessions  []CourtSession
	Err       error
}

// CourtDiffMsg carries diff content for a proposal.
type CourtDiffMsg struct {
	Content string
	Err     error
}

// CourtVoteResultMsg carries the result of a vote.
type CourtVoteResultMsg struct {
	Err error
}

// NewCourtDashboard creates a new court dashboard model.
func NewCourtDashboard() CourtDashboardModel {
	cols := []Column{
		{Title: "ID", Width: 10},
		{Title: "TITLE", Width: 30},
		{Title: "CATEGORY", Width: 14},
		{Title: "STATUS", Width: 12},
		{Title: "RISK", Width: 8},
		{Title: "ROUND", Width: 6},
		{Title: "UPDATED", Width: 12},
	}
	return CourtDashboardModel{
		table:      NewTable(cols, 15),
		diffViewer: NewDiffViewer(20),
		modal:      NewModal("Confirm Vote", 60),
		keys:       DefaultKeyMap(),
		view:       courtViewList,
	}
}

// Init starts the initial data load.
func (m CourtDashboardModel) Init() tea.Cmd {
	return func() tea.Msg {
		return CourtRefreshMsg{}
	}
}

// Update handles messages for the court dashboard.
func (m CourtDashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.Height = msg.Height - 12
		m.diffViewer.Height = msg.Height - 8
		return m, nil

	case CourtRefreshMsg:
		return m, m.loadData()

	case CourtDataMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.proposals = msg.Proposals
		m.sessions = msg.Sessions
		m.err = nil
		m.rebuildTable()
		return m, nil

	case CourtDiffMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.diffViewer.SetContent(msg.Content)
		m.view = courtViewDiff
		return m, nil

	case CourtVoteResultMsg:
		m.modal.Hide()
		m.view = courtViewList
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		return m, func() tea.Msg { return CourtRefreshMsg{} }

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m CourtDashboardModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Quit always works
	if key.Matches(msg, m.keys.Quit) {
		return m, tea.Quit
	}

	switch m.view {
	case courtViewList:
		return m.handleListKey(msg)
	case courtViewDetail:
		return m.handleDetailKey(msg)
	case courtViewDiff:
		return m.handleDiffKey(msg)
	case courtViewVoteConfirm:
		return m.handleVoteConfirmKey(msg)
	}

	return m, nil
}

func (m CourtDashboardModel) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.Down),
		key.Matches(msg, m.keys.PageUp), key.Matches(msg, m.keys.PageDown):
		m.table.Update(msg)
		return m, nil

	case key.Matches(msg, m.keys.Enter):
		if row := m.table.SelectedRow(); row != nil {
			m.selected = m.table.Cursor
			m.view = courtViewDetail
		}
		return m, nil

	case key.Matches(msg, m.keys.Refresh):
		return m, func() tea.Msg { return CourtRefreshMsg{} }

	case key.Matches(msg, m.keys.Approve):
		if m.table.Cursor < len(m.proposals) {
			m.voteAction = "approve"
			m.prevView = courtViewList
			m.voteProposalIdx = m.table.Cursor
			p := m.proposals[m.table.Cursor]
			m.modal.Show(fmt.Sprintf("Approve proposal %s?\n\n%s\n\nPress enter to confirm, esc to cancel.",
				Truncate(p.ID, 8), p.Title))
			m.view = courtViewVoteConfirm
		}
		return m, nil

	case key.Matches(msg, m.keys.Reject):
		if m.table.Cursor < len(m.proposals) {
			m.voteAction = "reject"
			m.prevView = courtViewList
			m.voteProposalIdx = m.table.Cursor
			p := m.proposals[m.table.Cursor]
			m.modal.Show(fmt.Sprintf("Reject proposal %s?\n\n%s\n\nPress enter to confirm, esc to cancel.",
				Truncate(p.ID, 8), p.Title))
			m.view = courtViewVoteConfirm
		}
		return m, nil
	}

	return m, nil
}

func (m CourtDashboardModel) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.view = courtViewList
		return m, nil

	case key.Matches(msg, m.keys.Tab):
		// Switch to diff view for selected proposal
		if m.selected < len(m.proposals) {
			return m, m.loadDiff(m.proposals[m.selected].ID)
		}
		return m, nil

	case key.Matches(msg, m.keys.Approve):
		if m.selected < len(m.proposals) {
			m.voteAction = "approve"
			m.prevView = courtViewDetail
			m.voteProposalIdx = m.selected
			p := m.proposals[m.selected]
			m.modal.Show(fmt.Sprintf("Approve proposal %s?\n\n%s\n\nPress enter to confirm, esc to cancel.",
				Truncate(p.ID, 8), p.Title))
			m.view = courtViewVoteConfirm
		}
		return m, nil

	case key.Matches(msg, m.keys.Reject):
		if m.selected < len(m.proposals) {
			m.voteAction = "reject"
			m.prevView = courtViewDetail
			m.voteProposalIdx = m.selected
			p := m.proposals[m.selected]
			m.modal.Show(fmt.Sprintf("Reject proposal %s?\n\n%s\n\nPress enter to confirm, esc to cancel.",
				Truncate(p.ID, 8), p.Title))
			m.view = courtViewVoteConfirm
		}
		return m, nil
	}

	return m, nil
}

func (m CourtDashboardModel) handleDiffKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.view = courtViewDetail
		return m, nil
	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.PageUp):
		m.diffViewer.ScrollUp(3)
		return m, nil
	case key.Matches(msg, m.keys.Down), key.Matches(msg, m.keys.PageDown):
		m.diffViewer.ScrollDown(3)
		return m, nil
	}
	return m, nil
}

func (m CourtDashboardModel) handleVoteConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.modal.Hide()
		m.view = m.prevView
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		if m.view == courtViewVoteConfirm && m.voteProposalIdx < len(m.proposals) {
			return m, m.castVote(m.proposals[m.voteProposalIdx].ID, m.voteAction == "approve")
		}
		return m, nil
	}
	return m, nil
}

// View renders the court dashboard.
func (m CourtDashboardModel) View() string {
	var b strings.Builder

	title := TitleStyle.Render("Court Review Dashboard")
	b.WriteString(title)
	b.WriteString("\n")

	if m.err != nil {
		b.WriteString(ErrorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n\n")
	}

	switch m.view {
	case courtViewList:
		b.WriteString(m.renderList())
	case courtViewDetail:
		b.WriteString(m.renderDetail())
	case courtViewDiff:
		b.WriteString(m.renderDiffView())
	case courtViewVoteConfirm:
		b.WriteString(m.renderList())
		b.WriteString("\n")
		b.WriteString(m.modal.View())
	}

	return b.String()
}

func (m CourtDashboardModel) renderList() string {
	var b strings.Builder

	// Session summary
	if len(m.sessions) > 0 {
		b.WriteString(SubtitleStyle.Render(fmt.Sprintf("Active Sessions: %d", len(m.sessions))))
		b.WriteString("\n\n")
	}

	b.WriteString(m.table.View())
	b.WriteString("\n")

	helpKeys := []key.Binding{
		m.keys.Up, m.keys.Down, m.keys.Enter,
		m.keys.Approve, m.keys.Reject,
		m.keys.Refresh, m.keys.Quit,
	}
	b.WriteString(HelpView(helpKeys...))

	return b.String()
}

func (m CourtDashboardModel) renderDetail() string {
	if m.selected >= len(m.proposals) {
		return MutedStyle.Render("No proposal selected")
	}

	p := m.proposals[m.selected]
	var b strings.Builder

	// Header
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Bold(true).Width(12).Render("Proposal:"),
		lipgloss.NewStyle().Render(p.ID),
	)
	b.WriteString(header)
	b.WriteString("\n")

	// Fields
	fields := []struct {
		label string
		value string
	}{
		{"Title", p.Title},
		{"Category", p.Category},
		{"Status", p.Status},
		{"Risk", p.Risk},
		{"Author", p.Author},
		{"Round", fmt.Sprintf("%d", p.Round)},
		{"Updated", p.Updated.Format("2006-01-02 15:04")},
	}
	for _, f := range fields {
		label := lipgloss.NewStyle().Bold(true).Width(12).Render(f.label + ":")
		value := f.value
		switch f.label {
		case "Status":
			value = StatusStyle(f.value).Render(f.value)
		case "Risk":
			value = RiskStyle(f.value).Render(f.value)
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, label, value))
		b.WriteString("\n")
	}

	// Session reviews
	session := m.findSession(p.ID)
	if session != nil {
		b.WriteString("\n")
		b.WriteString(SubtitleStyle.Render(fmt.Sprintf("Session: %s  State: %s",
			Truncate(session.SessionID, 8), session.State)))
		b.WriteString("\n\n")

		if len(session.Reviews) > 0 {
			// Review table
			headerLine := fmt.Sprintf("  %-15s %-10s %-6s %s",
				"PERSONA", "VERDICT", "RISK", "COMMENTS")
			b.WriteString(HeaderStyle.Render(headerLine))
			b.WriteString("\n")

			for _, r := range session.Reviews {
				verdictStyled := StatusStyle(string(r.Verdict)).Render(string(r.Verdict))
				comments := Truncate(r.Comments, 40)
				b.WriteString(fmt.Sprintf("  %-15s %-10s %-6.1f %s\n",
					r.Persona, verdictStyled, r.RiskScore, comments))
			}
		}

		if len(session.Reviews) > 0 {
			b.WriteString("\n")
			b.WriteString(SubtitleStyle.Render("Evidence:"))
			b.WriteString("\n")
			for _, r := range session.Reviews {
				if len(r.Evidence) > 0 {
					b.WriteString(fmt.Sprintf("  %s:\n", r.Persona))
					for _, e := range r.Evidence {
						b.WriteString(fmt.Sprintf("    - %s\n", Truncate(e, 70)))
					}
				}
			}
		}
	}

	b.WriteString("\n")
	helpKeys := []key.Binding{
		m.keys.Tab, m.keys.Approve, m.keys.Reject,
		m.keys.Back, m.keys.Quit,
	}
	b.WriteString(HelpView(helpKeys...))

	return BorderStyle.Render(b.String())
}

func (m CourtDashboardModel) renderDiffView() string {
	var b strings.Builder

	if m.selected < len(m.proposals) {
		p := m.proposals[m.selected]
		b.WriteString(SubtitleStyle.Render(fmt.Sprintf("Diff: %s — %s", Truncate(p.ID, 8), p.Title)))
		b.WriteString("\n\n")
	}

	b.WriteString(m.diffViewer.View())
	b.WriteString("\n")

	helpKeys := []key.Binding{
		m.keys.Up, m.keys.Down,
		m.keys.Back, m.keys.Quit,
	}
	b.WriteString(HelpView(helpKeys...))

	return b.String()
}

func (m CourtDashboardModel) findSession(proposalID string) *CourtSession {
	for i := range m.sessions {
		if m.sessions[i].ProposalID == proposalID {
			return &m.sessions[i]
		}
	}
	return nil
}

func (m *CourtDashboardModel) rebuildTable() {
	rows := make([]Row, len(m.proposals))
	for i, p := range m.proposals {
		rows[i] = Row{
			Truncate(p.ID, 8),
			Truncate(p.Title, 28),
			p.Category,
			p.Status,
			p.Risk,
			fmt.Sprintf("%d", p.Round),
			p.Updated.Format("01-02 15:04"),
		}
	}
	m.table.SetRows(rows)
}

func (m CourtDashboardModel) loadData() tea.Cmd {
	return func() tea.Msg {
		var proposals []CourtProposal
		var sessions []CourtSession
		var err error

		if m.LoadProposals != nil {
			proposals, err = m.LoadProposals()
			if err != nil {
				return CourtDataMsg{Err: err}
			}
		}
		if m.LoadSessions != nil {
			sessions, err = m.LoadSessions()
			if err != nil {
				return CourtDataMsg{Err: err}
			}
		}

		return CourtDataMsg{Proposals: proposals, Sessions: sessions}
	}
}

func (m CourtDashboardModel) loadDiff(proposalID string) tea.Cmd {
	return func() tea.Msg {
		if m.LoadDiff == nil {
			return CourtDiffMsg{Content: "(no diff loader configured)"}
		}
		content, err := m.LoadDiff(proposalID)
		if err != nil {
			return CourtDiffMsg{Err: err}
		}
		return CourtDiffMsg{Content: content}
	}
}

func (m CourtDashboardModel) castVote(proposalID string, approve bool) tea.Cmd {
	return func() tea.Msg {
		if m.CastVote == nil {
			return CourtVoteResultMsg{Err: fmt.Errorf("vote callback not configured")}
		}
		reason := "approved via dashboard"
		if !approve {
			reason = "rejected via dashboard"
		}
		err := m.CastVote(proposalID, approve, reason)
		return CourtVoteResultMsg{Err: err}
	}
}
