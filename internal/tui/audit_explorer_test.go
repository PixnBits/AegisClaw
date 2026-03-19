package tui

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleAuditEntries() []AuditEntry {
	now := time.Now()
	return []AuditEntry{
		{ID: "entry-001", PrevHash: "", Hash: "aabbcc11", Timestamp: now.Add(-2 * time.Hour), Payload: `{"action":"kernel_boot"}`, Valid: true},
		{ID: "entry-002", PrevHash: "aabbcc11", Hash: "ddeeff22", Timestamp: now.Add(-time.Hour), Payload: `{"action":"skill_activate","skill":"slack"}`, Valid: true},
		{ID: "entry-003", PrevHash: "ddeeff22", Hash: "112233ff", Timestamp: now, Payload: `{"action":"proposal_review","id":"prop-001"}`, Valid: true},
	}
}

func TestNewAuditExplorer(t *testing.T) {
	m := NewAuditExplorer()
	if m.view != auditViewList {
		t.Errorf("expected initial view list, got %d", m.view)
	}
	if len(m.table.Columns) != 5 {
		t.Errorf("expected 5 columns, got %d", len(m.table.Columns))
	}
}

func TestAuditExplorerInit(t *testing.T) {
	m := NewAuditExplorer()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should return a command")
	}
	msg := cmd()
	if _, ok := msg.(AuditRefreshMsg); !ok {
		t.Errorf("Init should produce AuditRefreshMsg, got %T", msg)
	}
}

func TestAuditExplorerDataLoad(t *testing.T) {
	m := NewAuditExplorer()
	m.LoadEntries = func() ([]AuditEntry, error) {
		return sampleAuditEntries(), nil
	}

	updated, cmd := m.Update(AuditRefreshMsg{})
	m = updated.(AuditExplorerModel)
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(AuditExplorerModel)

	if len(m.entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(m.entries))
	}
	if len(m.table.Rows) != 3 {
		t.Errorf("expected 3 table rows, got %d", len(m.table.Rows))
	}
}

func TestAuditExplorerDataLoadError(t *testing.T) {
	m := NewAuditExplorer()
	m.LoadEntries = func() ([]AuditEntry, error) {
		return nil, fmt.Errorf("file not found")
	}

	updated, cmd := m.Update(AuditRefreshMsg{})
	m = updated.(AuditExplorerModel)
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(AuditExplorerModel)

	if m.err == nil {
		t.Error("expected error")
	}
}

func TestAuditExplorerWindowSize(t *testing.T) {
	m := NewAuditExplorer()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(AuditExplorerModel)
	if m.width != 120 {
		t.Errorf("expected width 120, got %d", m.width)
	}
	if m.table.Height != 30 {
		t.Errorf("expected table height 30, got %d", m.table.Height)
	}
}

func TestAuditExplorerNavigation(t *testing.T) {
	m := NewAuditExplorer()
	m.entries = sampleAuditEntries()
	m.rebuildTable()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(AuditExplorerModel)
	if m.table.Cursor != 1 {
		t.Errorf("expected cursor 1, got %d", m.table.Cursor)
	}
}

func TestAuditExplorerEnterDetail(t *testing.T) {
	m := NewAuditExplorer()
	m.entries = sampleAuditEntries()
	m.rebuildTable()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(AuditExplorerModel)
	if m.view != auditViewDetail {
		t.Errorf("expected detail view, got %d", m.view)
	}
	if m.selected != 0 {
		t.Errorf("expected selected 0, got %d", m.selected)
	}
}

func TestAuditExplorerDetailBack(t *testing.T) {
	m := NewAuditExplorer()
	m.view = auditViewDetail
	m.selected = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(AuditExplorerModel)
	if m.view != auditViewList {
		t.Errorf("expected list view, got %d", m.view)
	}
}

func TestAuditExplorerSearchFlow(t *testing.T) {
	m := NewAuditExplorer()
	m.entries = sampleAuditEntries()
	m.rebuildTable()

	// Press '/' to enter search
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(AuditExplorerModel)
	if m.view != auditViewSearch {
		t.Errorf("expected search view, got %d", m.view)
	}

	// Type search query
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(AuditExplorerModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(AuditExplorerModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(AuditExplorerModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(AuditExplorerModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(AuditExplorerModel)

	if m.searchInput != "slack" {
		t.Errorf("expected search input 'slack', got %q", m.searchInput)
	}

	// Press enter to search
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(AuditExplorerModel)
	if m.view != auditViewList {
		t.Errorf("expected list view after search, got %d", m.view)
	}
	if m.searchQuery != "slack" {
		t.Errorf("expected search query 'slack', got %q", m.searchQuery)
	}
	if m.filtered == nil {
		t.Fatal("expected filtered to be set")
	}
	if len(m.filtered) != 1 {
		t.Errorf("expected 1 match, got %d", len(m.filtered))
	}
	if len(m.table.Rows) != 1 {
		t.Errorf("expected 1 table row, got %d", len(m.table.Rows))
	}
}

func TestAuditExplorerSearchCancel(t *testing.T) {
	m := NewAuditExplorer()
	m.view = auditViewSearch
	m.searchInput = "partial"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(AuditExplorerModel)
	if m.view != auditViewList {
		t.Errorf("expected list view after cancel, got %d", m.view)
	}
}

func TestAuditExplorerSearchBackspace(t *testing.T) {
	m := NewAuditExplorer()
	m.view = auditViewSearch
	m.searchInput = "test"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(AuditExplorerModel)
	if m.searchInput != "tes" {
		t.Errorf("expected 'tes' after backspace, got %q", m.searchInput)
	}
}

func TestAuditExplorerSearchBackspaceEmpty(t *testing.T) {
	m := NewAuditExplorer()
	m.view = auditViewSearch
	m.searchInput = ""

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(AuditExplorerModel)
	if m.searchInput != "" {
		t.Errorf("expected empty after backspace on empty, got %q", m.searchInput)
	}
}

func TestAuditExplorerVerify(t *testing.T) {
	m := NewAuditExplorer()
	m.entries = sampleAuditEntries()
	m.rebuildTable()
	m.VerifyChain = func() (uint64, error) {
		return 3, nil
	}

	// Press 'x' to verify
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(AuditExplorerModel)
	if cmd == nil {
		t.Fatal("expected verify command")
	}

	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(AuditExplorerModel)
	if m.view != auditViewVerify {
		t.Errorf("expected verify view, got %d", m.view)
	}
	if m.verifyCount != 3 {
		t.Errorf("expected 3 verified, got %d", m.verifyCount)
	}
	if m.verifyErr != "" {
		t.Errorf("expected no error, got %q", m.verifyErr)
	}
}

func TestAuditExplorerVerifyError(t *testing.T) {
	m := NewAuditExplorer()
	m.VerifyChain = func() (uint64, error) {
		return 2, fmt.Errorf("chain break at entry 3")
	}

	updated, _ := m.Update(AuditVerifyMsg{Count: 2, Err: fmt.Errorf("chain break at entry 3")})
	m = updated.(AuditExplorerModel)
	if m.view != auditViewVerify {
		t.Errorf("expected verify view, got %d", m.view)
	}
	if m.verifyErr == "" {
		t.Error("expected verify error")
	}
}

func TestAuditExplorerVerifyBack(t *testing.T) {
	m := NewAuditExplorer()
	m.view = auditViewVerify

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(AuditExplorerModel)
	if m.view != auditViewList {
		t.Errorf("expected list view after esc from verify, got %d", m.view)
	}
}

func TestAuditExplorerRollbackFlow(t *testing.T) {
	m := NewAuditExplorer()
	m.entries = sampleAuditEntries()
	m.rebuildTable()
	m.view = auditViewDetail
	m.selected = 1

	var rolledBackID string
	m.RollbackEntry = func(id string) error {
		rolledBackID = id
		return nil
	}

	// Press 'a' for rollback
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(AuditExplorerModel)
	if m.view != auditViewRollbackConfirm {
		t.Errorf("expected rollback confirm view, got %d", m.view)
	}
	if !m.modal.Visible {
		t.Error("expected modal visible")
	}

	// Confirm with enter
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(AuditExplorerModel)
	if cmd == nil {
		t.Fatal("expected rollback command")
	}

	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(AuditExplorerModel)

	if rolledBackID != "entry-002" {
		t.Errorf("expected rollback on entry-002, got %s", rolledBackID)
	}
	if m.view != auditViewList {
		t.Errorf("expected return to list, got %d", m.view)
	}
}

func TestAuditExplorerRollbackCancel(t *testing.T) {
	m := NewAuditExplorer()
	m.entries = sampleAuditEntries()
	m.view = auditViewRollbackConfirm
	m.modal.Visible = true

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(AuditExplorerModel)
	if m.view != auditViewDetail {
		t.Errorf("expected detail view after cancel, got %d", m.view)
	}
	if m.modal.Visible {
		t.Error("expected modal hidden")
	}
}

func TestAuditExplorerRollbackError(t *testing.T) {
	m := NewAuditExplorer()
	updated, _ := m.Update(AuditRollbackMsg{Err: fmt.Errorf("rollback failed")})
	m = updated.(AuditExplorerModel)
	if m.err == nil {
		t.Error("expected error from rollback failure")
	}
}

func TestAuditExplorerViewList(t *testing.T) {
	m := NewAuditExplorer()
	m.entries = sampleAuditEntries()
	m.rebuildTable()

	output := m.View()
	if !containsStr(output, "Audit Explorer") {
		t.Error("expected title")
	}
	if !containsStr(output, "Entries: 3") {
		t.Error("expected entry count")
	}
}

func TestAuditExplorerViewDetail(t *testing.T) {
	m := NewAuditExplorer()
	m.entries = sampleAuditEntries()
	m.view = auditViewDetail
	m.selected = 0

	output := m.View()
	if !containsStr(output, "entry-001") {
		t.Error("expected entry ID in detail")
	}
	if !containsStr(output, "kernel_boot") {
		t.Error("expected payload in detail")
	}
}

func TestAuditExplorerViewVerify(t *testing.T) {
	m := NewAuditExplorer()
	m.view = auditViewVerify
	m.verifyCount = 10

	output := m.View()
	if !containsStr(output, "10 entries verified") {
		t.Error("expected verified count in output")
	}
}

func TestAuditExplorerViewVerifyError(t *testing.T) {
	m := NewAuditExplorer()
	m.view = auditViewVerify
	m.verifyErr = "chain break"
	m.verifyCount = 5

	output := m.View()
	if !containsStr(output, "FAIL") {
		t.Error("expected FAIL in output")
	}
	if !containsStr(output, "chain break") {
		t.Error("expected error message in output")
	}
}

func TestAuditExplorerViewSearch(t *testing.T) {
	m := NewAuditExplorer()
	m.view = auditViewSearch
	m.searchInput = "test"

	output := m.View()
	if !containsStr(output, "test") {
		t.Error("expected search input in output")
	}
}

func TestAuditExplorerViewError(t *testing.T) {
	m := NewAuditExplorer()
	m.err = fmt.Errorf("test error")
	output := m.View()
	if !containsStr(output, "test error") {
		t.Error("expected error in output")
	}
}

func TestAuditExplorerQuit(t *testing.T) {
	m := NewAuditExplorer()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
}

func TestAuditExplorerRefresh(t *testing.T) {
	m := NewAuditExplorer()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatal("expected refresh command")
	}
	msg := cmd()
	if _, ok := msg.(AuditRefreshMsg); !ok {
		t.Errorf("expected AuditRefreshMsg, got %T", msg)
	}
}

func TestAuditExplorerNilCallbacks(t *testing.T) {
	m := NewAuditExplorer()

	cmd := m.loadData()
	msg := cmd()
	if dataMsg, ok := msg.(AuditDataMsg); !ok || dataMsg.Err != nil {
		t.Errorf("expected clean data msg with nil callback")
	}

	cmd = m.verifyChain()
	msg = cmd()
	if verifyMsg, ok := msg.(AuditVerifyMsg); !ok || verifyMsg.Err == nil {
		t.Error("expected error with nil verify callback")
	}

	cmd = m.rollback("test")
	msg = cmd()
	if rbMsg, ok := msg.(AuditRollbackMsg); !ok || rbMsg.Err == nil {
		t.Error("expected error with nil rollback callback")
	}
}

func TestAuditExplorerResolveIndex(t *testing.T) {
	m := NewAuditExplorer()
	m.entries = sampleAuditEntries()
	m.filtered = []int{2, 0}

	if idx := m.resolveIndex(0); idx != 2 {
		t.Errorf("expected resolved index 2, got %d", idx)
	}
	if idx := m.resolveIndex(1); idx != 0 {
		t.Errorf("expected resolved index 0, got %d", idx)
	}

	// Without filter
	m.filtered = nil
	if idx := m.resolveIndex(1); idx != 1 {
		t.Errorf("expected passthrough index 1, got %d", idx)
	}
}

func TestAuditExplorerFilteredSearch(t *testing.T) {
	m := NewAuditExplorer()
	m.entries = sampleAuditEntries()
	m.searchQuery = "proposal"
	m.applyFilter()

	if len(m.filtered) != 1 {
		t.Errorf("expected 1 match for 'proposal', got %d", len(m.filtered))
	}
	if m.filtered[0] != 2 {
		t.Errorf("expected match at index 2, got %d", m.filtered[0])
	}
}

func TestAuditExplorerEmptyFilter(t *testing.T) {
	m := NewAuditExplorer()
	m.entries = sampleAuditEntries()
	m.searchQuery = ""
	m.applyFilter()

	if m.filtered != nil {
		t.Error("expected nil filter for empty query")
	}
}

func TestAuditExplorerSearchEnterFromDetail(t *testing.T) {
	m := NewAuditExplorer()
	m.entries = sampleAuditEntries()
	m.rebuildTable()
	m.filtered = []int{1}
	m.rebuildTable()

	// Enter with filtered view should select correct entry
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(AuditExplorerModel)
	if m.selected != 1 {
		t.Errorf("expected selected index 1 (resolved from filtered), got %d", m.selected)
	}
}
