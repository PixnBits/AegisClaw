package tui

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleProposals() []CourtProposal {
	now := time.Now()
	return []CourtProposal{
		{ID: "prop-001", Title: "Add Slack skill", Category: "new_skill", Status: "submitted", Risk: "medium", Author: "alice", Round: 1, Updated: now},
		{ID: "prop-002", Title: "Fix kernel bug", Category: "kernel_patch", Status: "in_review", Risk: "high", Author: "bob", Round: 2, Updated: now.Add(-time.Hour)},
		{ID: "prop-003", Title: "Delete old skill", Category: "delete_skill", Status: "approved", Risk: "low", Author: "carol", Round: 1, Updated: now.Add(-2 * time.Hour)},
	}
}

func sampleSessions() []CourtSession {
	return []CourtSession{
		{
			SessionID:  "sess-001",
			ProposalID: "prop-002",
			State:      "reviewing",
			Round:      2,
			RiskScore:  6.5,
			Personas:   []string{"CISO", "Coder"},
			Reviews: []CourtReview{
				{Persona: "CISO", Verdict: "reject", RiskScore: 7.0, Comments: "Too risky", Evidence: []string{"uses root"}, Round: 1},
				{Persona: "Coder", Verdict: "approve", RiskScore: 3.0, Comments: "Looks fine", Evidence: []string{"clean code"}, Round: 1},
			},
		},
	}
}

func TestNewCourtDashboard(t *testing.T) {
	m := NewCourtDashboard()
	if m.view != courtViewList {
		t.Errorf("expected initial view to be list, got %d", m.view)
	}
	if len(m.table.Columns) != 7 {
		t.Errorf("expected 7 columns, got %d", len(m.table.Columns))
	}
}

func TestCourtDashboardInit(t *testing.T) {
	m := NewCourtDashboard()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should return a command")
	}
	msg := cmd()
	if _, ok := msg.(CourtRefreshMsg); !ok {
		t.Errorf("Init command should produce CourtRefreshMsg, got %T", msg)
	}
}

func TestCourtDashboardDataLoad(t *testing.T) {
	m := NewCourtDashboard()
	m.LoadProposals = func() ([]CourtProposal, error) {
		return sampleProposals(), nil
	}
	m.LoadSessions = func() ([]CourtSession, error) {
		return sampleSessions(), nil
	}

	// Simulate refresh
	updated, cmd := m.Update(CourtRefreshMsg{})
	m = updated.(CourtDashboardModel)
	if cmd == nil {
		t.Fatal("expected a load command")
	}

	// Simulate data arriving
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(CourtDashboardModel)

	if len(m.proposals) != 3 {
		t.Errorf("expected 3 proposals, got %d", len(m.proposals))
	}
	if len(m.sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(m.sessions))
	}
	if len(m.table.Rows) != 3 {
		t.Errorf("expected 3 table rows, got %d", len(m.table.Rows))
	}
}

func TestCourtDashboardDataLoadError(t *testing.T) {
	m := NewCourtDashboard()
	m.LoadProposals = func() ([]CourtProposal, error) {
		return nil, fmt.Errorf("store unavailable")
	}

	updated, cmd := m.Update(CourtRefreshMsg{})
	m = updated.(CourtDashboardModel)
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(CourtDashboardModel)

	if m.err == nil {
		t.Error("expected error to be set")
	}
}

func TestCourtDashboardWindowSize(t *testing.T) {
	m := NewCourtDashboard()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(CourtDashboardModel)

	if m.width != 120 {
		t.Errorf("expected width 120, got %d", m.width)
	}
	if m.height != 40 {
		t.Errorf("expected height 40, got %d", m.height)
	}
	if m.table.Height != 28 {
		t.Errorf("expected table height 28, got %d", m.table.Height)
	}
}

func TestCourtDashboardNavigation(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.rebuildTable()

	// Navigate down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(CourtDashboardModel)
	if m.table.Cursor != 1 {
		t.Errorf("expected cursor at 1 after down, got %d", m.table.Cursor)
	}

	// Navigate up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(CourtDashboardModel)
	if m.table.Cursor != 0 {
		t.Errorf("expected cursor at 0 after up, got %d", m.table.Cursor)
	}
}

func TestCourtDashboardEnterDetail(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.sessions = sampleSessions()
	m.rebuildTable()

	// Press enter to view detail
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(CourtDashboardModel)
	if m.view != courtViewDetail {
		t.Errorf("expected detail view, got %d", m.view)
	}
	if m.selected != 0 {
		t.Errorf("expected selected 0, got %d", m.selected)
	}
}

func TestCourtDashboardDetailBack(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.rebuildTable()
	m.view = courtViewDetail
	m.selected = 0

	// Press esc to go back
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(CourtDashboardModel)
	if m.view != courtViewList {
		t.Errorf("expected list view after esc, got %d", m.view)
	}
}

func TestCourtDashboardDiffView(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.rebuildTable()
	m.view = courtViewDetail
	m.selected = 0
	m.LoadDiff = func(id string) (string, error) {
		return "+added line\n-removed line", nil
	}

	// Press tab to load diff
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(CourtDashboardModel)
	if cmd == nil {
		t.Fatal("expected diff load command")
	}

	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(CourtDashboardModel)
	if m.view != courtViewDiff {
		t.Errorf("expected diff view, got %d", m.view)
	}
	if len(m.diffViewer.Lines) != 2 {
		t.Errorf("expected 2 diff lines, got %d", len(m.diffViewer.Lines))
	}
}

func TestCourtDashboardDiffNavigation(t *testing.T) {
	m := NewCourtDashboard()
	m.view = courtViewDiff
	m.diffViewer.SetContent("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10")
	m.diffViewer.Height = 3

	// Scroll down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(CourtDashboardModel)
	if m.diffViewer.Offset != 3 {
		t.Errorf("expected offset 3 after scroll down, got %d", m.diffViewer.Offset)
	}

	// Scroll back up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(CourtDashboardModel)
	if m.diffViewer.Offset != 0 {
		t.Errorf("expected offset 0 after scroll up, got %d", m.diffViewer.Offset)
	}
}

func TestCourtDashboardDiffBack(t *testing.T) {
	m := NewCourtDashboard()
	m.view = courtViewDiff

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(CourtDashboardModel)
	if m.view != courtViewDetail {
		t.Errorf("expected detail view after esc from diff, got %d", m.view)
	}
}

func TestCourtDashboardVoteApprove(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.rebuildTable()

	// Press 'a' to approve
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(CourtDashboardModel)
	if m.view != courtViewVoteConfirm {
		t.Errorf("expected vote confirm view, got %d", m.view)
	}
	if m.voteAction != "approve" {
		t.Errorf("expected approve action, got %s", m.voteAction)
	}
	if !m.modal.Visible {
		t.Error("expected modal to be visible")
	}
}

func TestCourtDashboardVoteReject(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.rebuildTable()

	// Press 'x' to reject
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(CourtDashboardModel)
	if m.view != courtViewVoteConfirm {
		t.Errorf("expected vote confirm view, got %d", m.view)
	}
	if m.voteAction != "reject" {
		t.Errorf("expected reject action, got %s", m.voteAction)
	}
}

func TestCourtDashboardVoteCancel(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.rebuildTable()
	m.view = courtViewVoteConfirm
	m.modal.Visible = true

	// Press esc to cancel
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(CourtDashboardModel)
	if m.view != courtViewList {
		t.Errorf("expected list view after cancel, got %d", m.view)
	}
	if m.modal.Visible {
		t.Error("expected modal to be hidden")
	}
}

func TestCourtDashboardVoteConfirmExecutes(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.rebuildTable()
	m.view = courtViewVoteConfirm
	m.voteAction = "approve"

	var votedID string
	var votedApprove bool
	m.CastVote = func(id string, approve bool, reason string) error {
		votedID = id
		votedApprove = approve
		return nil
	}

	// Press enter to confirm vote
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(CourtDashboardModel)
	if cmd == nil {
		t.Fatal("expected vote command")
	}

	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(CourtDashboardModel)

	if votedID != "prop-001" {
		t.Errorf("expected vote on prop-001, got %s", votedID)
	}
	if !votedApprove {
		t.Error("expected approve vote")
	}
	if m.view != courtViewList {
		t.Errorf("expected return to list view, got %d", m.view)
	}
}

func TestCourtDashboardVoteError(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.rebuildTable()
	m.view = courtViewVoteConfirm
	m.voteAction = "reject"
	m.CastVote = func(id string, approve bool, reason string) error {
		return fmt.Errorf("vote denied")
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(CourtDashboardModel)
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(CourtDashboardModel)

	if m.err == nil {
		t.Error("expected error to be set")
	}
}

func TestCourtDashboardViewList(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.sessions = sampleSessions()
	m.rebuildTable()

	output := m.View()
	if output == "" {
		t.Error("expected non-empty view output")
	}
	if !containsStr(output, "Court Review Dashboard") {
		t.Error("expected title in output")
	}
	if !containsStr(output, "Active Sessions: 1") {
		t.Error("expected session count in output")
	}
}

func TestCourtDashboardViewDetail(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.sessions = sampleSessions()
	m.rebuildTable()
	m.view = courtViewDetail
	m.selected = 1 // prop-002 which has a session

	output := m.View()
	if !containsStr(output, "prop-002") {
		t.Error("expected proposal ID in detail view")
	}
	if !containsStr(output, "CISO") {
		t.Error("expected persona name in detail view")
	}
}

func TestCourtDashboardViewError(t *testing.T) {
	m := NewCourtDashboard()
	m.err = fmt.Errorf("test error")

	output := m.View()
	if !containsStr(output, "test error") {
		t.Error("expected error in output")
	}
}

func TestCourtDashboardFindSession(t *testing.T) {
	m := NewCourtDashboard()
	m.sessions = sampleSessions()

	s := m.findSession("prop-002")
	if s == nil {
		t.Fatal("expected to find session for prop-002")
	}
	if s.SessionID != "sess-001" {
		t.Errorf("expected session sess-001, got %s", s.SessionID)
	}

	s = m.findSession("nonexistent")
	if s != nil {
		t.Error("expected nil for nonexistent proposal")
	}
}

func TestCourtDashboardQuit(t *testing.T) {
	m := NewCourtDashboard()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
}

func TestCourtDashboardRebuildTable(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.rebuildTable()

	if len(m.table.Rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(m.table.Rows))
	}
	// First row should contain truncated ID
	if m.table.Rows[0][0] != "prop-001" {
		t.Errorf("expected prop-001, got %s", m.table.Rows[0][0])
	}
}

func TestCourtDashboardRefreshFromList(t *testing.T) {
	m := NewCourtDashboard()
	m.LoadProposals = func() ([]CourtProposal, error) {
		return sampleProposals(), nil
	}
	m.LoadSessions = func() ([]CourtSession, error) {
		return nil, nil
	}

	// Press 'r' to refresh
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatal("expected refresh command")
	}
	msg := cmd()
	if _, ok := msg.(CourtRefreshMsg); !ok {
		t.Errorf("expected CourtRefreshMsg, got %T", msg)
	}
}

func TestCourtDashboardApproveFromDetail(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.rebuildTable()
	m.view = courtViewDetail
	m.selected = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(CourtDashboardModel)
	if m.view != courtViewVoteConfirm {
		t.Errorf("expected vote confirm from detail, got %d", m.view)
	}
	if m.voteAction != "approve" {
		t.Errorf("expected approve, got %s", m.voteAction)
	}
}

func TestCourtDashboardRejectFromDetail(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.rebuildTable()
	m.view = courtViewDetail
	m.selected = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(CourtDashboardModel)
	if m.view != courtViewVoteConfirm {
		t.Errorf("expected vote confirm from detail, got %d", m.view)
	}
	if m.voteAction != "reject" {
		t.Errorf("expected reject, got %s", m.voteAction)
	}
}

func TestCourtDashboardDiffLoadError(t *testing.T) {
	m := NewCourtDashboard()
	m.LoadDiff = func(id string) (string, error) {
		return "", fmt.Errorf("diff failed")
	}

	updated, _ := m.Update(CourtDiffMsg{Err: fmt.Errorf("diff failed")})
	m = updated.(CourtDashboardModel)
	if m.err == nil {
		t.Error("expected error to be set on diff load failure")
	}
}

func TestCourtDashboardNilCallbacks(t *testing.T) {
	m := NewCourtDashboard()

	// loadData with nil callbacks should not panic
	cmd := m.loadData()
	msg := cmd()
	dataMsg, ok := msg.(CourtDataMsg)
	if !ok {
		t.Fatalf("expected CourtDataMsg, got %T", msg)
	}
	if dataMsg.Err != nil {
		t.Errorf("expected no error with nil callbacks, got %v", dataMsg.Err)
	}

	// loadDiff with nil callback
	cmd = m.loadDiff("test")
	msg = cmd()
	diffMsg, ok := msg.(CourtDiffMsg)
	if !ok {
		t.Fatalf("expected CourtDiffMsg, got %T", msg)
	}
	if diffMsg.Err != nil {
		t.Errorf("expected no error with nil diff callback, got %v", diffMsg.Err)
	}

	// castVote with nil callback
	cmd = m.castVote("test", true)
	msg = cmd()
	voteMsg, ok := msg.(CourtVoteResultMsg)
	if !ok {
		t.Fatalf("expected CourtVoteResultMsg, got %T", msg)
	}
	if voteMsg.Err == nil {
		t.Error("expected error with nil vote callback")
	}
}

func TestCourtDashboardVoteConfirmModalContent(t *testing.T) {
	m := NewCourtDashboard()
	m.proposals = sampleProposals()
	m.rebuildTable()

	// Approve shows modal with proposal title
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(CourtDashboardModel)
	if !containsStr(m.modal.Content, "Add Slack skill") {
		t.Error("expected modal to contain proposal title")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && searchStr(s, sub)
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
