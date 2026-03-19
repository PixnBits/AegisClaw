package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// statusPane tracks which panel is focused.
type statusPane int

const (
	statusPaneSandboxes statusPane = iota
	statusPaneSkills
	statusPaneDetail
)

// SandboxRow holds data for one sandbox in the status dashboard.
type SandboxRow struct {
	ID        string
	Name      string
	State     string
	VCPUs     int64
	MemoryMB  int64
	PID       int
	StartedAt *time.Time
	GuestIP   string
}

// SkillRow holds data for one skill in the status dashboard.
type SkillRow struct {
	Name        string
	SandboxID   string
	State       string
	ActivatedAt *time.Time
	Version     int
}

// StatusInfo holds kernel-level status information.
type StatusInfo struct {
	PublicKeyHex   string
	AuditEntries   uint64
	AuditChainHead string
	RegistryRoot   string
}

// StatusDashboardModel is the bubbletea Model for the skill status dashboard.
type StatusDashboardModel struct {
	sandboxTable Table
	skillTable   Table
	keys         KeyMap
	pane         statusPane
	status       StatusInfo
	sandboxes    []SandboxRow
	skills       []SkillRow
	width        int
	height       int
	err          error

	// Callbacks
	LoadStatus   func() (StatusInfo, []SandboxRow, []SkillRow, error)
	StopSandbox  func(id string) error
	StartSandbox func(id string) error
}

// StatusRefreshMsg triggers a data reload.
type StatusRefreshMsg struct{}

// StatusDataMsg carries refreshed data.
type StatusDataMsg struct {
	Info      StatusInfo
	Sandboxes []SandboxRow
	Skills    []SkillRow
	Err       error
}

// StatusActionMsg carries the result of a start/stop action.
type StatusActionMsg struct {
	Err error
}

// NewStatusDashboard creates a new status dashboard model.
func NewStatusDashboard() StatusDashboardModel {
	sbCols := []Column{
		{Title: "ID", Width: 10},
		{Title: "NAME", Width: 18},
		{Title: "STATE", Width: 10},
		{Title: "VCPU", Width: 5},
		{Title: "MEM", Width: 6},
		{Title: "PID", Width: 8},
		{Title: "IP", Width: 14},
		{Title: "STARTED", Width: 12},
	}
	skCols := []Column{
		{Title: "NAME", Width: 20},
		{Title: "SANDBOX", Width: 10},
		{Title: "STATE", Width: 10},
		{Title: "VERSION", Width: 8},
		{Title: "ACTIVATED", Width: 16},
	}
	return StatusDashboardModel{
		sandboxTable: NewTable(sbCols, 8),
		skillTable:   NewTable(skCols, 8),
		keys:         DefaultKeyMap(),
		pane:         statusPaneSandboxes,
	}
}

// Init starts the initial data load.
func (m StatusDashboardModel) Init() tea.Cmd {
	return func() tea.Msg {
		return StatusRefreshMsg{}
	}
}

// Update handles messages.
func (m StatusDashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		half := (msg.Height - 14) / 2
		if half < 3 {
			half = 3
		}
		m.sandboxTable.Height = half
		m.skillTable.Height = half
		return m, nil

	case StatusRefreshMsg:
		return m, m.loadData()

	case StatusDataMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.status = msg.Info
		m.sandboxes = msg.Sandboxes
		m.skills = msg.Skills
		m.err = nil
		m.rebuildTables()
		return m, nil

	case StatusActionMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		return m, func() tea.Msg { return StatusRefreshMsg{} }

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m StatusDashboardModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Quit) {
		return m, tea.Quit
	}

	switch {
	case key.Matches(msg, m.keys.Tab):
		switch m.pane {
		case statusPaneSandboxes:
			m.sandboxTable.Blur()
			m.skillTable.Focus()
			m.pane = statusPaneSkills
		case statusPaneSkills:
			m.skillTable.Blur()
			m.sandboxTable.Focus()
			m.pane = statusPaneSandboxes
		}
		return m, nil

	case key.Matches(msg, m.keys.Refresh):
		return m, func() tea.Msg { return StatusRefreshMsg{} }
	}

	// Route navigation to focused table
	switch m.pane {
	case statusPaneSandboxes:
		return m.handleSandboxKey(msg)
	case statusPaneSkills:
		m.skillTable.Update(msg)
		return m, nil
	}

	return m, nil
}

func (m StatusDashboardModel) handleSandboxKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.Down),
		key.Matches(msg, m.keys.PageUp), key.Matches(msg, m.keys.PageDown):
		m.sandboxTable.Update(msg)
		return m, nil

	case key.Matches(msg, m.keys.Approve): // 'a' = start
		if row := m.sandboxTable.SelectedRow(); row != nil && m.sandboxTable.Cursor < len(m.sandboxes) {
			sb := m.sandboxes[m.sandboxTable.Cursor]
			if sb.State == "stopped" || sb.State == "created" {
				return m, m.startSandbox(sb.ID)
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Reject): // 'x' = stop
		if row := m.sandboxTable.SelectedRow(); row != nil && m.sandboxTable.Cursor < len(m.sandboxes) {
			sb := m.sandboxes[m.sandboxTable.Cursor]
			if sb.State == "running" {
				return m, m.stopSandbox(sb.ID)
			}
		}
		return m, nil
	}

	return m, nil
}

// View renders the status dashboard.
func (m StatusDashboardModel) View() string {
	var b strings.Builder

	title := TitleStyle.Render("AegisClaw Status Dashboard")
	b.WriteString(title)
	b.WriteString("\n")

	if m.err != nil {
		b.WriteString(ErrorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n\n")
	}

	// Kernel info bar
	b.WriteString(m.renderKernelInfo())
	b.WriteString("\n\n")

	// Sandbox panel
	sbTitle := "Sandboxes"
	if m.pane == statusPaneSandboxes {
		sbTitle = SelectedStyle.Render("▸ Sandboxes")
	} else {
		sbTitle = SubtitleStyle.Render("  Sandboxes")
	}
	b.WriteString(sbTitle)
	b.WriteString(fmt.Sprintf(" (%d)\n", len(m.sandboxes)))
	b.WriteString(m.sandboxTable.View())
	b.WriteString("\n")

	// Skills panel
	skTitle := "Skills"
	if m.pane == statusPaneSkills {
		skTitle = SelectedStyle.Render("▸ Skills")
	} else {
		skTitle = SubtitleStyle.Render("  Skills")
	}
	b.WriteString(skTitle)
	b.WriteString(fmt.Sprintf(" (%d)\n", len(m.skills)))
	b.WriteString(m.skillTable.View())
	b.WriteString("\n")

	// Help bar
	helpKeys := []key.Binding{
		m.keys.Up, m.keys.Down, m.keys.Tab,
		m.keys.Approve, m.keys.Reject,
		m.keys.Refresh, m.keys.Quit,
	}
	b.WriteString(HelpView(helpKeys...))

	return b.String()
}

func (m StatusDashboardModel) renderKernelInfo() string {
	items := []string{}
	if m.status.PublicKeyHex != "" {
		items = append(items, fmt.Sprintf("Key: %s", Truncate(m.status.PublicKeyHex, 16)))
	}
	items = append(items, fmt.Sprintf("Audit: %d entries", m.status.AuditEntries))
	if m.status.AuditChainHead != "" {
		items = append(items, fmt.Sprintf("Chain: %s", Truncate(m.status.AuditChainHead, 16)))
	}
	if m.status.RegistryRoot != "" {
		items = append(items, fmt.Sprintf("Registry: %s", Truncate(m.status.RegistryRoot, 16)))
	}

	infoStyle := lipgloss.NewStyle().
		Foreground(ColorText).
		Background(ColorSurface).
		Padding(0, 1)

	return infoStyle.Render(strings.Join(items, "  │  "))
}

func (m *StatusDashboardModel) rebuildTables() {
	// Sandbox rows
	sbRows := make([]Row, len(m.sandboxes))
	for i, sb := range m.sandboxes {
		started := ""
		if sb.StartedAt != nil {
			started = sb.StartedAt.Format("15:04:05")
		}
		sbRows[i] = Row{
			Truncate(sb.ID, 8),
			Truncate(sb.Name, 16),
			sb.State,
			fmt.Sprintf("%d", sb.VCPUs),
			fmt.Sprintf("%d", sb.MemoryMB),
			fmt.Sprintf("%d", sb.PID),
			sb.GuestIP,
			started,
		}
	}
	m.sandboxTable.SetRows(sbRows)

	// Skill rows
	skRows := make([]Row, len(m.skills))
	for i, sk := range m.skills {
		activated := ""
		if sk.ActivatedAt != nil {
			activated = sk.ActivatedAt.Format("2006-01-02 15:04")
		}
		skRows[i] = Row{
			Truncate(sk.Name, 18),
			Truncate(sk.SandboxID, 8),
			sk.State,
			fmt.Sprintf("%d", sk.Version),
			activated,
		}
	}
	m.skillTable.SetRows(skRows)
}

func (m StatusDashboardModel) loadData() tea.Cmd {
	return func() tea.Msg {
		if m.LoadStatus == nil {
			return StatusDataMsg{}
		}
		info, sandboxes, skills, err := m.LoadStatus()
		if err != nil {
			return StatusDataMsg{Err: err}
		}
		return StatusDataMsg{Info: info, Sandboxes: sandboxes, Skills: skills}
	}
}

func (m StatusDashboardModel) startSandbox(id string) tea.Cmd {
	return func() tea.Msg {
		if m.StartSandbox == nil {
			return StatusActionMsg{Err: fmt.Errorf("start callback not configured")}
		}
		return StatusActionMsg{Err: m.StartSandbox(id)}
	}
}

func (m StatusDashboardModel) stopSandbox(id string) tea.Cmd {
	return func() tea.Msg {
		if m.StopSandbox == nil {
			return StatusActionMsg{Err: fmt.Errorf("stop callback not configured")}
		}
		return StatusActionMsg{Err: m.StopSandbox(id)}
	}
}
