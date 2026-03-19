package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewTable_Columns(t *testing.T) {
	cols := []Column{
		{Title: "ID", Width: 10},
		{Title: "Name", Width: 20},
	}
	tbl := NewTable(cols, 5)
	if len(tbl.Columns) != 2 {
		t.Errorf("expected 2 columns, got %d", len(tbl.Columns))
	}
	if tbl.Height != 5 {
		t.Errorf("expected height 5, got %d", tbl.Height)
	}
}

func TestTable_SetRows(t *testing.T) {
	tbl := NewTable([]Column{{Title: "A", Width: 10}}, 5)
	tbl.SetRows([]Row{{"one"}, {"two"}, {"three"}})
	if len(tbl.Rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(tbl.Rows))
	}
}

func TestTable_SelectedRow(t *testing.T) {
	tbl := NewTable([]Column{{Title: "A", Width: 10}}, 5)
	tbl.SetRows([]Row{{"one"}, {"two"}, {"three"}})
	tbl.Cursor = 1
	row := tbl.SelectedRow()
	if row == nil || row[0] != "two" {
		t.Errorf("expected selected row 'two', got %v", row)
	}
}

func TestTable_SelectedRow_Empty(t *testing.T) {
	tbl := NewTable([]Column{{Title: "A", Width: 10}}, 5)
	if tbl.SelectedRow() != nil {
		t.Error("expected nil for empty table")
	}
}

func TestTable_Navigation(t *testing.T) {
	tbl := NewTable([]Column{{Title: "A", Width: 10}}, 5)
	tbl.SetRows([]Row{{"one"}, {"two"}, {"three"}})

	// Move down
	tbl.Update(tea.KeyMsg{Type: tea.KeyDown})
	if tbl.Cursor != 1 {
		t.Errorf("after down: expected cursor 1, got %d", tbl.Cursor)
	}

	tbl.Update(tea.KeyMsg{Type: tea.KeyDown})
	if tbl.Cursor != 2 {
		t.Errorf("after 2x down: expected cursor 2, got %d", tbl.Cursor)
	}

	// Can't go past end
	tbl.Update(tea.KeyMsg{Type: tea.KeyDown})
	if tbl.Cursor != 2 {
		t.Errorf("after 3x down: expected cursor 2, got %d", tbl.Cursor)
	}

	// Move up
	tbl.Update(tea.KeyMsg{Type: tea.KeyUp})
	if tbl.Cursor != 1 {
		t.Errorf("after up: expected cursor 1, got %d", tbl.Cursor)
	}
}

func TestTable_Focus(t *testing.T) {
	tbl := NewTable([]Column{{Title: "A", Width: 10}}, 5)
	if !tbl.Focused() {
		t.Error("table should start focused")
	}
	tbl.Blur()
	if tbl.Focused() {
		t.Error("table should be blurred")
	}
	tbl.Focus()
	if !tbl.Focused() {
		t.Error("table should be focused again")
	}
}

func TestTable_View_Header(t *testing.T) {
	tbl := NewTable([]Column{
		{Title: "NAME", Width: 15},
		{Title: "STATUS", Width: 10},
	}, 3)
	tbl.SetRows([]Row{{"alpha", "ok"}, {"beta", "err"}})
	view := tbl.View()
	if !strings.Contains(view, "NAME") {
		t.Error("table view should contain header 'NAME'")
	}
	if !strings.Contains(view, "alpha") {
		t.Error("table view should contain row data")
	}
}

func TestTable_Scroll(t *testing.T) {
	tbl := NewTable([]Column{{Title: "A", Width: 10}}, 2)
	rows := make([]Row, 10)
	for i := range rows {
		rows[i] = Row{strings.Repeat("x", i+1)}
	}
	tbl.SetRows(rows)

	// Page down
	tbl.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if tbl.Cursor != 2 {
		t.Errorf("after pgdn: expected cursor 2, got %d", tbl.Cursor)
	}
}

func TestSpinner_View(t *testing.T) {
	s := NewSpinner("loading...")
	view := s.View()
	if !strings.Contains(view, "loading...") {
		t.Error("spinner should show message")
	}

	s.Done = true
	view = s.View()
	if !strings.Contains(view, "loading...") {
		t.Error("done spinner should still show message")
	}
}

func TestModal_ShowHide(t *testing.T) {
	m := NewModal("Confirm", 40)
	if m.Visible {
		t.Error("modal should start hidden")
	}
	if m.View() != "" {
		t.Error("hidden modal should render empty")
	}

	m.Show("Are you sure?")
	if !m.Visible {
		t.Error("modal should be visible after Show")
	}
	view := m.View()
	if !strings.Contains(view, "Confirm") {
		t.Error("modal should show title")
	}
	if !strings.Contains(view, "Are you sure?") {
		t.Error("modal should show content")
	}

	m.Hide()
	if m.Visible {
		t.Error("modal should be hidden after Hide")
	}
}

func TestDiffViewer_Empty(t *testing.T) {
	dv := NewDiffViewer(10)
	view := dv.View()
	if !strings.Contains(view, "no diff") {
		t.Error("empty diff viewer should show placeholder")
	}
}

func TestDiffViewer_Content(t *testing.T) {
	dv := NewDiffViewer(10)
	dv.SetContent("--- a/file.go\n+++ b/file.go\n@@ -1,3 +1,4 @@\n context\n-old line\n+new line\n+added line")
	view := dv.View()
	if !strings.Contains(view, "old line") {
		t.Error("diff viewer should show removed lines")
	}
	if !strings.Contains(view, "new line") {
		t.Error("diff viewer should show added lines")
	}
}

func TestDiffViewer_Scroll(t *testing.T) {
	dv := NewDiffViewer(3)
	dv.SetContent("line1\nline2\nline3\nline4\nline5\nline6")
	dv.ScrollDown(2)
	if dv.Offset != 2 {
		t.Errorf("expected offset 2, got %d", dv.Offset)
	}
	dv.ScrollUp(1)
	if dv.Offset != 1 {
		t.Errorf("expected offset 1, got %d", dv.Offset)
	}
	dv.ScrollUp(10) // Should clamp to 0
	if dv.Offset != 0 {
		t.Errorf("expected offset 0 after over-scroll up, got %d", dv.Offset)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 7, "hello.."},
		{"ab", 2, "ab"},
		{"abcd", 3, "a.."},
	}
	for _, tc := range cases {
		got := Truncate(tc.input, tc.max)
		if got != tc.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.input, tc.max, got, tc.want)
		}
	}
}

func TestDefaultKeyMap(t *testing.T) {
	km := DefaultKeyMap()
	if km.Quit.Keys() == nil {
		t.Error("quit key should have bindings")
	}
	if km.Up.Keys() == nil {
		t.Error("up key should have bindings")
	}
}

func TestHelpView(t *testing.T) {
	km := DefaultKeyMap()
	view := HelpView(km.Up, km.Down, km.Quit)
	if len(view) == 0 {
		t.Error("help view should not be empty")
	}
}

func TestRiskStyle(t *testing.T) {
	_ = RiskStyle("low")
	_ = RiskStyle("medium")
	_ = RiskStyle("high")
	_ = RiskStyle("critical")
	_ = RiskStyle("unknown")
}

func TestStatusStyle(t *testing.T) {
	_ = StatusStyle("approved")
	_ = StatusStyle("rejected")
	_ = StatusStyle("in_review")
	_ = StatusStyle("draft")
	_ = StatusStyle("unknown")
}

func TestSandboxStateStyle(t *testing.T) {
	_ = SandboxStateStyle("running")
	_ = SandboxStateStyle("stopped")
	_ = SandboxStateStyle("error")
	_ = SandboxStateStyle("unknown")
}
