package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------- Key Bindings ----------

// KeyMap defines the standard keyboard shortcuts across all TUI views.
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Enter    key.Binding
	Back     key.Binding
	Quit     key.Binding
	Help     key.Binding
	Tab      key.Binding
	Approve  key.Binding
	Reject   key.Binding
	Refresh  key.Binding
	Search   key.Binding
	PageUp   key.Binding
	PageDown key.Binding
}

// DefaultKeyMap provides standard AegisClaw TUI keybindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Tab:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next panel")),
		Approve:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "approve")),
		Reject:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "reject")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Search:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "page down")),
	}
}

// HelpView renders the help footer from a KeyMap.
func HelpView(keys ...key.Binding) string {
	var parts []string
	for _, k := range keys {
		help := k.Help()
		parts = append(parts, fmt.Sprintf("%s %s", MutedStyle.Render(help.Key), help.Desc))
	}
	return HelpStyle.Render(strings.Join(parts, "  "))
}

// ---------- Table Component ----------

// Column defines a table column.
type Column struct {
	Title string
	Width int
}

// Row is a table row (slice of cell strings).
type Row []string

// Table is a selectable data table with keyboard navigation.
type Table struct {
	Columns  []Column
	Rows     []Row
	Cursor   int
	Offset   int
	Height   int
	Width    int
	focused  bool
}

// NewTable creates a Table with the given columns.
func NewTable(columns []Column, height int) Table {
	return Table{
		Columns: columns,
		Height:  height,
		focused: true,
	}
}

// SetRows replaces the table data.
func (t *Table) SetRows(rows []Row) {
	t.Rows = rows
	if t.Cursor >= len(rows) && len(rows) > 0 {
		t.Cursor = len(rows) - 1
	}
	if t.Cursor < 0 {
		t.Cursor = 0
	}
}

// SelectedRow returns the currently selected row, or nil.
func (t *Table) SelectedRow() Row {
	if t.Cursor >= 0 && t.Cursor < len(t.Rows) {
		return t.Rows[t.Cursor]
	}
	return nil
}

// Focus sets focus state.
func (t *Table) Focus() { t.focused = true }

// Blur removes focus state.
func (t *Table) Blur() { t.focused = false }

// Focused returns whether the table is focused.
func (t *Table) Focused() bool { return t.focused }

// Update handles keyboard events for the table.
func (t *Table) Update(msg tea.Msg) {
	km := DefaultKeyMap()
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(keyMsg, km.Up):
			if t.Cursor > 0 {
				t.Cursor--
			}
			if t.Cursor < t.Offset {
				t.Offset = t.Cursor
			}
		case key.Matches(keyMsg, km.Down):
			if t.Cursor < len(t.Rows)-1 {
				t.Cursor++
			}
			visibleEnd := t.Offset + t.Height - 1
			if t.Cursor > visibleEnd {
				t.Offset = t.Cursor - t.Height + 1
			}
		case key.Matches(keyMsg, km.PageUp):
			t.Cursor -= t.Height
			if t.Cursor < 0 {
				t.Cursor = 0
			}
			if t.Cursor < t.Offset {
				t.Offset = t.Cursor
			}
		case key.Matches(keyMsg, km.PageDown):
			t.Cursor += t.Height
			if t.Cursor >= len(t.Rows) {
				t.Cursor = len(t.Rows) - 1
			}
			if t.Cursor < 0 {
				t.Cursor = 0
			}
			visibleEnd := t.Offset + t.Height - 1
			if t.Cursor > visibleEnd {
				t.Offset = t.Cursor - t.Height + 1
			}
		}
	}
}

// View renders the table as a string.
func (t *Table) View() string {
	var b strings.Builder

	// Header row
	var headerCells []string
	for _, col := range t.Columns {
		cell := HeaderStyle.Width(col.Width).Render(col.Title)
		headerCells = append(headerCells, cell)
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, headerCells...))
	b.WriteString("\n")

	// Data rows
	visibleEnd := t.Offset + t.Height
	if visibleEnd > len(t.Rows) {
		visibleEnd = len(t.Rows)
	}

	for i := t.Offset; i < visibleEnd; i++ {
		row := t.Rows[i]
		var cells []string
		for j, col := range t.Columns {
			val := ""
			if j < len(row) {
				val = row[j]
			}
			// Truncate to width
			if len(val) > col.Width-1 {
				val = val[:col.Width-2] + ".."
			}

			style := lipgloss.NewStyle().Width(col.Width)
			if i == t.Cursor && t.focused {
				style = style.Bold(true).Foreground(ColorPrimary)
			}
			cells = append(cells, style.Render(val))
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cells...))
		b.WriteString("\n")
	}

	// Scroll indicator
	if len(t.Rows) > t.Height {
		info := MutedStyle.Render(fmt.Sprintf(" %d/%d ", t.Cursor+1, len(t.Rows)))
		b.WriteString(info)
	}

	return b.String()
}

// ---------- Spinner Component ----------

// SpinnerModel wraps a bubbles spinner with a message.
type SpinnerModel struct {
	Spinner spinner.Model
	Message string
	Done    bool
}

// NewSpinner creates a new spinner with a message.
func NewSpinner(message string) SpinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(ColorPrimary)
	return SpinnerModel{
		Spinner: s,
		Message: message,
	}
}

// Init returns the spinner tick command.
func (m SpinnerModel) Init() tea.Cmd {
	return m.Spinner.Tick
}

// Update handles spinner tick messages.
func (m SpinnerModel) Update(msg tea.Msg) (SpinnerModel, tea.Cmd) {
	var cmd tea.Cmd
	m.Spinner, cmd = m.Spinner.Update(msg)
	return m, cmd
}

// View renders the spinner + message.
func (m SpinnerModel) View() string {
	if m.Done {
		return StatusApproved.Render("✓ " + m.Message)
	}
	return m.Spinner.View() + " " + m.Message
}

// ---------- Modal Component ----------

// Modal renders a centered overlay dialog.
type Modal struct {
	Title   string
	Content string
	Width   int
	Visible bool
}

// NewModal creates a modal dialog.
func NewModal(title string, width int) Modal {
	return Modal{
		Title: title,
		Width: width,
	}
}

// Show makes the modal visible with the given content.
func (m *Modal) Show(content string) {
	m.Content = content
	m.Visible = true
}

// Hide closes the modal.
func (m *Modal) Hide() {
	m.Visible = false
}

// View renders the modal. Returns empty string when hidden.
func (m *Modal) View() string {
	if !m.Visible {
		return ""
	}

	titleLine := TitleStyle.Render(m.Title)
	contentBlock := lipgloss.NewStyle().
		Width(m.Width - 6).
		Render(m.Content)

	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(ColorAccent).
		Padding(1, 2).
		Width(m.Width).
		Render(titleLine + "\n\n" + contentBlock)
}

// ---------- DiffViewer Component ----------

// DiffViewer renders a unified diff with syntax coloring.
type DiffViewer struct {
	Lines  []string
	Offset int
	Height int
}

// NewDiffViewer creates a diff viewer.
func NewDiffViewer(height int) DiffViewer {
	return DiffViewer{Height: height}
}

// SetContent parses a unified diff string into lines.
func (d *DiffViewer) SetContent(diff string) {
	d.Lines = strings.Split(diff, "\n")
	d.Offset = 0
}

// ScrollUp moves the viewport up.
func (d *DiffViewer) ScrollUp(n int) {
	d.Offset -= n
	if d.Offset < 0 {
		d.Offset = 0
	}
}

// ScrollDown moves the viewport down.
func (d *DiffViewer) ScrollDown(n int) {
	d.Offset += n
	max := len(d.Lines) - d.Height
	if max < 0 {
		max = 0
	}
	if d.Offset > max {
		d.Offset = max
	}
}

// View renders the visible portion of the diff.
func (d *DiffViewer) View() string {
	if len(d.Lines) == 0 {
		return MutedStyle.Render("(no diff)")
	}

	addStyle := lipgloss.NewStyle().Foreground(ColorSecondary)
	delStyle := lipgloss.NewStyle().Foreground(ColorDanger)
	hunkStyle := lipgloss.NewStyle().Foreground(ColorAccent)

	var b strings.Builder
	end := d.Offset + d.Height
	if end > len(d.Lines) {
		end = len(d.Lines)
	}

	for i := d.Offset; i < end; i++ {
		line := d.Lines[i]
		switch {
		case strings.HasPrefix(line, "+"):
			b.WriteString(addStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			b.WriteString(delStyle.Render(line))
		case strings.HasPrefix(line, "@@"):
			b.WriteString(hunkStyle.Render(line))
		default:
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	if len(d.Lines) > d.Height {
		b.WriteString(MutedStyle.Render(fmt.Sprintf("  lines %d-%d of %d", d.Offset+1, end, len(d.Lines))))
	}

	return b.String()
}

// ---------- Truncate Helper ----------

// Truncate shortens s to max characters, appending ".." if truncated.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 2 {
		return s[:max]
	}
	return s[:max-2] + ".."
}
