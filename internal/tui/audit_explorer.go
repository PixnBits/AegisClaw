package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// auditView tracks the active panel.
type auditView int

const (
	auditViewList auditView = iota
	auditViewDetail
	auditViewSearch
	auditViewVerify
	auditViewRollbackConfirm
)

// AuditEntry holds data for one audit log entry.
type AuditEntry struct {
	ID        string
	PrevHash  string
	Hash      string
	Timestamp time.Time
	Payload   string
	Valid     bool
}

// AuditExplorerModel is the bubbletea Model for the audit explorer.
type AuditExplorerModel struct {
	entries     []AuditEntry
	filtered    []int // indices into entries matching search
	table       Table
	modal       Modal
	keys        KeyMap
	view        auditView
	selected    int
	searchQuery string
	searchInput string
	verifyCount int
	verifyErr   string
	width       int
	height      int
	err         error

	// Callbacks
	LoadEntries   func() ([]AuditEntry, error)
	VerifyChain   func() (uint64, error)
	RollbackEntry func(entryID string) error
}

// AuditRefreshMsg triggers entry reload.
type AuditRefreshMsg struct{}

// AuditDataMsg carries loaded entries.
type AuditDataMsg struct {
	Entries []AuditEntry
	Err     error
}

// AuditVerifyMsg carries verification results.
type AuditVerifyMsg struct {
	Count uint64
	Err   error
}

// AuditRollbackMsg carries rollback results.
type AuditRollbackMsg struct {
	Err error
}

// NewAuditExplorer creates a new audit explorer model.
func NewAuditExplorer() AuditExplorerModel {
	cols := []Column{
		{Title: "#", Width: 6},
		{Title: "ID", Width: 10},
		{Title: "TIMESTAMP", Width: 20},
		{Title: "HASH", Width: 18},
		{Title: "PAYLOAD", Width: 40},
	}
	return AuditExplorerModel{
		table:    NewTable(cols, 20),
		modal:    NewModal("Confirm Rollback", 60),
		keys:     DefaultKeyMap(),
		view:     auditViewList,
		filtered: nil,
	}
}

// Init starts the initial data load.
func (m AuditExplorerModel) Init() tea.Cmd {
	return func() tea.Msg {
		return AuditRefreshMsg{}
	}
}

// Update handles messages.
func (m AuditExplorerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.Height = msg.Height - 10
		return m, nil

	case AuditRefreshMsg:
		return m, m.loadData()

	case AuditDataMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.entries = msg.Entries
		m.filtered = nil
		m.searchQuery = ""
		m.err = nil
		m.rebuildTable()
		return m, nil

	case AuditVerifyMsg:
		if msg.Err != nil {
			m.verifyErr = msg.Err.Error()
		} else {
			m.verifyErr = ""
		}
		m.verifyCount = int(msg.Count)
		m.view = auditViewVerify
		return m, nil

	case AuditRollbackMsg:
		m.modal.Hide()
		m.view = auditViewList
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		return m, func() tea.Msg { return AuditRefreshMsg{} }

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m AuditExplorerModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Quit) && m.view != auditViewSearch {
		return m, tea.Quit
	}

	switch m.view {
	case auditViewList:
		return m.handleListKey(msg)
	case auditViewDetail:
		return m.handleDetailKey(msg)
	case auditViewSearch:
		return m.handleSearchKey(msg)
	case auditViewVerify:
		return m.handleVerifyKey(msg)
	case auditViewRollbackConfirm:
		return m.handleRollbackKey(msg)
	}

	return m, nil
}

func (m AuditExplorerModel) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.Down),
		key.Matches(msg, m.keys.PageUp), key.Matches(msg, m.keys.PageDown):
		m.table.Update(msg)
		return m, nil

	case key.Matches(msg, m.keys.Enter):
		if row := m.table.SelectedRow(); row != nil {
			m.selected = m.resolveIndex(m.table.Cursor)
			m.view = auditViewDetail
		}
		return m, nil

	case key.Matches(msg, m.keys.Search):
		m.view = auditViewSearch
		m.searchInput = ""
		return m, nil

	case key.Matches(msg, m.keys.Refresh):
		return m, func() tea.Msg { return AuditRefreshMsg{} }

	case key.Matches(msg, m.keys.Reject): // 'x' = verify chain
		return m, m.verifyChain()
	}

	return m, nil
}

func (m AuditExplorerModel) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.view = auditViewList
		return m, nil

	case key.Matches(msg, m.keys.Approve): // 'a' = rollback to this entry
		if m.selected < len(m.entries) {
			e := m.entries[m.selected]
			m.modal.Show(fmt.Sprintf("Rollback to entry %s?\n\nHash: %s\nTime: %s\n\nPress enter to confirm, esc to cancel.",
				Truncate(e.ID, 8), Truncate(e.Hash, 16), e.Timestamp.Format(time.RFC3339)))
			m.view = auditViewRollbackConfirm
		}
		return m, nil
	}

	return m, nil
}

func (m AuditExplorerModel) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.view = auditViewList
		return m, nil
	case tea.KeyEnter:
		m.searchQuery = m.searchInput
		m.applyFilter()
		m.rebuildTable()
		m.view = auditViewList
		return m, nil
	case tea.KeyBackspace:
		if len(m.searchInput) > 0 {
			m.searchInput = m.searchInput[:len(m.searchInput)-1]
		}
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.searchInput += string(msg.Runes)
		}
		return m, nil
	}
}

func (m AuditExplorerModel) handleVerifyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Back) || key.Matches(msg, m.keys.Enter) {
		m.view = auditViewList
		return m, nil
	}
	return m, nil
}

func (m AuditExplorerModel) handleRollbackKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.modal.Hide()
		m.view = auditViewDetail
		return m, nil
	case key.Matches(msg, m.keys.Enter):
		if m.selected < len(m.entries) {
			return m, m.rollback(m.entries[m.selected].ID)
		}
		return m, nil
	}
	return m, nil
}

// View renders the audit explorer.
func (m AuditExplorerModel) View() string {
	var b strings.Builder

	title := TitleStyle.Render("Audit Explorer")
	b.WriteString(title)
	b.WriteString("\n")

	if m.err != nil {
		b.WriteString(ErrorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n\n")
	}

	switch m.view {
	case auditViewList:
		b.WriteString(m.renderList())
	case auditViewDetail:
		b.WriteString(m.renderDetail())
	case auditViewSearch:
		b.WriteString(m.renderSearch())
	case auditViewVerify:
		b.WriteString(m.renderVerify())
	case auditViewRollbackConfirm:
		b.WriteString(m.renderDetail())
		b.WriteString("\n")
		b.WriteString(m.modal.View())
	}

	return b.String()
}

func (m AuditExplorerModel) renderList() string {
	var b strings.Builder

	info := fmt.Sprintf("Entries: %d", len(m.entries))
	if m.searchQuery != "" {
		matching := len(m.entries)
		if m.filtered != nil {
			matching = len(m.filtered)
		}
		info += fmt.Sprintf("  Search: %q (%d matches)", m.searchQuery, matching)
	}
	b.WriteString(SubtitleStyle.Render(info))
	b.WriteString("\n\n")

	b.WriteString(m.table.View())
	b.WriteString("\n")

	helpKeys := []key.Binding{
		m.keys.Up, m.keys.Down, m.keys.Enter,
		m.keys.Search, m.keys.Reject,
		m.keys.Refresh, m.keys.Quit,
	}
	b.WriteString(HelpView(helpKeys...))

	return b.String()
}

func (m AuditExplorerModel) renderDetail() string {
	if m.selected >= len(m.entries) {
		return MutedStyle.Render("No entry selected")
	}

	e := m.entries[m.selected]
	var b strings.Builder

	fields := []struct {
		label string
		value string
	}{
		{"ID", e.ID},
		{"Timestamp", e.Timestamp.Format(time.RFC3339)},
		{"Hash", e.Hash},
		{"Prev Hash", e.PrevHash},
	}
	for _, f := range fields {
		label := lipgloss.NewStyle().Bold(true).Width(12).Render(f.label + ":")
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, label, f.value))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(SubtitleStyle.Render("Payload:"))
	b.WriteString("\n")

	payloadStyle := lipgloss.NewStyle().
		Foreground(ColorText).
		MaxWidth(m.width - 4)
	b.WriteString(payloadStyle.Render(e.Payload))
	b.WriteString("\n\n")

	helpKeys := []key.Binding{
		m.keys.Approve, m.keys.Back, m.keys.Quit,
	}
	b.WriteString(HelpView(helpKeys...))

	return BorderStyle.Render(b.String())
}

func (m AuditExplorerModel) renderSearch() string {
	var b strings.Builder
	b.WriteString(SubtitleStyle.Render("Search audit entries:"))
	b.WriteString("\n\n")

	prompt := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render("> ")
	cursor := lipgloss.NewStyle().Foreground(ColorAccent).Render("_")
	b.WriteString(prompt + m.searchInput + cursor)
	b.WriteString("\n\n")
	b.WriteString(MutedStyle.Render("Enter to search, Esc to cancel"))

	return b.String()
}

func (m AuditExplorerModel) renderVerify() string {
	var b strings.Builder

	b.WriteString(SubtitleStyle.Render("Chain Verification"))
	b.WriteString("\n\n")

	if m.verifyErr != "" {
		b.WriteString(StatusRejected.Render("FAIL"))
		b.WriteString(fmt.Sprintf("  Verified %d entries before error\n", m.verifyCount))
		b.WriteString(ErrorStyle.Render(m.verifyErr))
	} else {
		b.WriteString(StatusApproved.Render("OK"))
		b.WriteString(fmt.Sprintf("  All %d entries verified\n", m.verifyCount))
	}

	b.WriteString("\n\n")
	b.WriteString(MutedStyle.Render("Press esc or enter to return"))

	return BorderStyle.Render(b.String())
}

func (m *AuditExplorerModel) rebuildTable() {
	indices := m.filtered
	if indices == nil {
		indices = make([]int, len(m.entries))
		for i := range m.entries {
			indices[i] = i
		}
	}

	rows := make([]Row, len(indices))
	for i, idx := range indices {
		e := m.entries[idx]
		payload := e.Payload
		if len(payload) > 38 {
			payload = payload[:36] + ".."
		}
		rows[i] = Row{
			fmt.Sprintf("%d", idx+1),
			Truncate(e.ID, 8),
			e.Timestamp.Format("01-02 15:04:05"),
			Truncate(e.Hash, 16),
			payload,
		}
	}
	m.table.SetRows(rows)
}

func (m *AuditExplorerModel) applyFilter() {
	if m.searchQuery == "" {
		m.filtered = nil
		return
	}

	query := strings.ToLower(m.searchQuery)
	var matches []int
	for i, e := range m.entries {
		if strings.Contains(strings.ToLower(e.ID), query) ||
			strings.Contains(strings.ToLower(e.Hash), query) ||
			strings.Contains(strings.ToLower(e.Payload), query) {
			matches = append(matches, i)
		}
	}
	m.filtered = matches
}

func (m AuditExplorerModel) resolveIndex(cursor int) int {
	if m.filtered != nil && cursor < len(m.filtered) {
		return m.filtered[cursor]
	}
	return cursor
}

func (m AuditExplorerModel) loadData() tea.Cmd {
	return func() tea.Msg {
		if m.LoadEntries == nil {
			return AuditDataMsg{}
		}
		entries, err := m.LoadEntries()
		if err != nil {
			return AuditDataMsg{Err: err}
		}
		return AuditDataMsg{Entries: entries}
	}
}

func (m AuditExplorerModel) verifyChain() tea.Cmd {
	return func() tea.Msg {
		if m.VerifyChain == nil {
			return AuditVerifyMsg{Err: fmt.Errorf("verify callback not configured")}
		}
		count, err := m.VerifyChain()
		return AuditVerifyMsg{Count: count, Err: err}
	}
}

func (m AuditExplorerModel) rollback(entryID string) tea.Cmd {
	return func() tea.Msg {
		if m.RollbackEntry == nil {
			return AuditRollbackMsg{Err: fmt.Errorf("rollback callback not configured")}
		}
		return AuditRollbackMsg{Err: m.RollbackEntry(entryID)}
	}
}
