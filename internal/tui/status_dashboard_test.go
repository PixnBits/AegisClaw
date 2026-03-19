package tui

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleSandboxRows() []SandboxRow {
	now := time.Now()
	return []SandboxRow{
		{ID: "sb-001", Name: "slack-skill", State: "running", VCPUs: 2, MemoryMB: 512, PID: 1234, StartedAt: &now, GuestIP: "10.0.0.2"},
		{ID: "sb-002", Name: "builder-vm", State: "stopped", VCPUs: 4, MemoryMB: 1024, PID: 0, GuestIP: "10.0.0.6"},
		{ID: "sb-003", Name: "reviewer-1", State: "created", VCPUs: 1, MemoryMB: 256, PID: 0, GuestIP: ""},
	}
}

func sampleSkillRows() []SkillRow {
	now := time.Now()
	return []SkillRow{
		{Name: "slack-api", SandboxID: "sb-001", State: "active", ActivatedAt: &now, Version: 3},
		{Name: "git-helper", SandboxID: "sb-002", State: "inactive", Version: 1},
	}
}

func sampleStatusInfo() StatusInfo {
	return StatusInfo{
		PublicKeyHex:   "abcdef1234567890",
		AuditEntries:  42,
		AuditChainHead: "deadbeef12345678",
		RegistryRoot:  "cafebabe87654321",
	}
}

func TestNewStatusDashboard(t *testing.T) {
	m := NewStatusDashboard()
	if m.pane != statusPaneSandboxes {
		t.Errorf("expected initial pane to be sandboxes, got %d", m.pane)
	}
	if len(m.sandboxTable.Columns) != 8 {
		t.Errorf("expected 8 sandbox columns, got %d", len(m.sandboxTable.Columns))
	}
	if len(m.skillTable.Columns) != 5 {
		t.Errorf("expected 5 skill columns, got %d", len(m.skillTable.Columns))
	}
}

func TestStatusDashboardInit(t *testing.T) {
	m := NewStatusDashboard()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should return a command")
	}
	msg := cmd()
	if _, ok := msg.(StatusRefreshMsg); !ok {
		t.Errorf("Init should produce StatusRefreshMsg, got %T", msg)
	}
}

func TestStatusDashboardDataLoad(t *testing.T) {
	m := NewStatusDashboard()
	m.LoadStatus = func() (StatusInfo, []SandboxRow, []SkillRow, error) {
		return sampleStatusInfo(), sampleSandboxRows(), sampleSkillRows(), nil
	}

	updated, cmd := m.Update(StatusRefreshMsg{})
	m = updated.(StatusDashboardModel)
	if cmd == nil {
		t.Fatal("expected load command")
	}

	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(StatusDashboardModel)

	if len(m.sandboxes) != 3 {
		t.Errorf("expected 3 sandboxes, got %d", len(m.sandboxes))
	}
	if len(m.skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(m.skills))
	}
	if m.status.AuditEntries != 42 {
		t.Errorf("expected 42 audit entries, got %d", m.status.AuditEntries)
	}
	if len(m.sandboxTable.Rows) != 3 {
		t.Errorf("expected 3 sandbox table rows, got %d", len(m.sandboxTable.Rows))
	}
	if len(m.skillTable.Rows) != 2 {
		t.Errorf("expected 2 skill table rows, got %d", len(m.skillTable.Rows))
	}
}

func TestStatusDashboardDataLoadError(t *testing.T) {
	m := NewStatusDashboard()
	m.LoadStatus = func() (StatusInfo, []SandboxRow, []SkillRow, error) {
		return StatusInfo{}, nil, nil, fmt.Errorf("connection failed")
	}

	updated, cmd := m.Update(StatusRefreshMsg{})
	m = updated.(StatusDashboardModel)
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(StatusDashboardModel)

	if m.err == nil {
		t.Error("expected error to be set")
	}
}

func TestStatusDashboardWindowSize(t *testing.T) {
	m := NewStatusDashboard()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	m = updated.(StatusDashboardModel)

	if m.width != 100 {
		t.Errorf("expected width 100, got %d", m.width)
	}
	if m.height != 50 {
		t.Errorf("expected height 50, got %d", m.height)
	}
}

func TestStatusDashboardTabSwitch(t *testing.T) {
	m := NewStatusDashboard()
	m.sandboxes = sampleSandboxRows()
	m.skills = sampleSkillRows()
	m.rebuildTables()

	// Initial pane is sandboxes
	if m.pane != statusPaneSandboxes {
		t.Fatalf("expected initial pane sandboxes, got %d", m.pane)
	}

	// Tab to skills
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(StatusDashboardModel)
	if m.pane != statusPaneSkills {
		t.Errorf("expected skills pane after tab, got %d", m.pane)
	}

	// Tab back to sandboxes
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(StatusDashboardModel)
	if m.pane != statusPaneSandboxes {
		t.Errorf("expected sandboxes pane after second tab, got %d", m.pane)
	}
}

func TestStatusDashboardSandboxNavigation(t *testing.T) {
	m := NewStatusDashboard()
	m.sandboxes = sampleSandboxRows()
	m.rebuildTables()

	// Navigate down in sandbox table
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(StatusDashboardModel)
	if m.sandboxTable.Cursor != 1 {
		t.Errorf("expected cursor at 1, got %d", m.sandboxTable.Cursor)
	}

	// Navigate up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(StatusDashboardModel)
	if m.sandboxTable.Cursor != 0 {
		t.Errorf("expected cursor at 0, got %d", m.sandboxTable.Cursor)
	}
}

func TestStatusDashboardSkillNavigation(t *testing.T) {
	m := NewStatusDashboard()
	m.skills = sampleSkillRows()
	m.rebuildTables()
	m.pane = statusPaneSkills
	m.sandboxTable.Blur()
	m.skillTable.Focus()

	// Navigate down in skill table
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(StatusDashboardModel)
	if m.skillTable.Cursor != 1 {
		t.Errorf("expected skill cursor at 1, got %d", m.skillTable.Cursor)
	}
}

func TestStatusDashboardStartSandbox(t *testing.T) {
	m := NewStatusDashboard()
	m.sandboxes = []SandboxRow{
		{ID: "sb-002", Name: "stopped-vm", State: "stopped"},
	}
	m.rebuildTables()

	var startedID string
	m.StartSandbox = func(id string) error {
		startedID = id
		return nil
	}

	// Press 'a' to start
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(StatusDashboardModel)
	if cmd == nil {
		t.Fatal("expected start command")
	}
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(StatusDashboardModel)

	if startedID != "sb-002" {
		t.Errorf("expected start on sb-002, got %s", startedID)
	}
}

func TestStatusDashboardStartRunningNoOp(t *testing.T) {
	m := NewStatusDashboard()
	m.sandboxes = []SandboxRow{
		{ID: "sb-001", Name: "running-vm", State: "running"},
	}
	m.rebuildTables()

	m.StartSandbox = func(id string) error {
		t.Error("start should not be called for running sandbox")
		return nil
	}

	// 'a' on a running sandbox should do nothing
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd != nil {
		t.Error("expected no command for start on running sandbox")
	}
}

func TestStatusDashboardStopSandbox(t *testing.T) {
	m := NewStatusDashboard()
	m.sandboxes = []SandboxRow{
		{ID: "sb-001", Name: "running-vm", State: "running"},
	}
	m.rebuildTables()

	var stoppedID string
	m.StopSandbox = func(id string) error {
		stoppedID = id
		return nil
	}

	// Press 'x' to stop
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(StatusDashboardModel)
	if cmd == nil {
		t.Fatal("expected stop command")
	}
	msg := cmd()
	updated, _ = m.Update(msg)

	if stoppedID != "sb-001" {
		t.Errorf("expected stop on sb-001, got %s", stoppedID)
	}
}

func TestStatusDashboardStopStoppedNoOp(t *testing.T) {
	m := NewStatusDashboard()
	m.sandboxes = []SandboxRow{
		{ID: "sb-002", Name: "stopped-vm", State: "stopped"},
	}
	m.rebuildTables()

	m.StopSandbox = func(id string) error {
		t.Error("stop should not be called for stopped sandbox")
		return nil
	}

	// 'x' on a stopped sandbox should do nothing
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd != nil {
		t.Error("expected no command for stop on stopped sandbox")
	}
}

func TestStatusDashboardRefresh(t *testing.T) {
	m := NewStatusDashboard()
	m.LoadStatus = func() (StatusInfo, []SandboxRow, []SkillRow, error) {
		return sampleStatusInfo(), nil, nil, nil
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatal("expected refresh command")
	}
	msg := cmd()
	if _, ok := msg.(StatusRefreshMsg); !ok {
		t.Errorf("expected StatusRefreshMsg, got %T", msg)
	}
}

func TestStatusDashboardQuit(t *testing.T) {
	m := NewStatusDashboard()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
}

func TestStatusDashboardViewOutput(t *testing.T) {
	m := NewStatusDashboard()
	m.status = sampleStatusInfo()
	m.sandboxes = sampleSandboxRows()
	m.skills = sampleSkillRows()
	m.rebuildTables()

	output := m.View()
	if output == "" {
		t.Error("expected non-empty view output")
	}
	if !containsStr(output, "AegisClaw Status Dashboard") {
		t.Error("expected title in output")
	}
	if !containsStr(output, "Sandboxes") {
		t.Error("expected sandboxes section")
	}
	if !containsStr(output, "Skills") {
		t.Error("expected skills section")
	}
	if !containsStr(output, "Audit: 42") {
		t.Error("expected audit entry count in kernel info")
	}
}

func TestStatusDashboardViewError(t *testing.T) {
	m := NewStatusDashboard()
	m.err = fmt.Errorf("test error")
	output := m.View()
	if !containsStr(output, "test error") {
		t.Error("expected error in output")
	}
}

func TestStatusDashboardNilCallbacks(t *testing.T) {
	m := NewStatusDashboard()

	// loadData with nil LoadStatus
	cmd := m.loadData()
	msg := cmd()
	dataMsg, ok := msg.(StatusDataMsg)
	if !ok {
		t.Fatalf("expected StatusDataMsg, got %T", msg)
	}
	if dataMsg.Err != nil {
		t.Errorf("expected no error with nil callback, got %v", dataMsg.Err)
	}

	// start with nil callback
	cmd = m.startSandbox("test")
	msg = cmd()
	actionMsg, ok := msg.(StatusActionMsg)
	if !ok {
		t.Fatalf("expected StatusActionMsg, got %T", msg)
	}
	if actionMsg.Err == nil {
		t.Error("expected error with nil start callback")
	}

	// stop with nil callback
	cmd = m.stopSandbox("test")
	msg = cmd()
	actionMsg, ok = msg.(StatusActionMsg)
	if !ok {
		t.Fatalf("expected StatusActionMsg, got %T", msg)
	}
	if actionMsg.Err == nil {
		t.Error("expected error with nil stop callback")
	}
}

func TestStatusDashboardActionError(t *testing.T) {
	m := NewStatusDashboard()
	updated, _ := m.Update(StatusActionMsg{Err: fmt.Errorf("action failed")})
	m = updated.(StatusDashboardModel)
	if m.err == nil {
		t.Error("expected error to be set from action")
	}
}

func TestStatusDashboardActionSuccess(t *testing.T) {
	m := NewStatusDashboard()
	m.LoadStatus = func() (StatusInfo, []SandboxRow, []SkillRow, error) {
		return StatusInfo{}, nil, nil, nil
	}

	updated, cmd := m.Update(StatusActionMsg{})
	m = updated.(StatusDashboardModel)
	if cmd == nil {
		t.Fatal("expected refresh command after successful action")
	}
	msg := cmd()
	if _, ok := msg.(StatusRefreshMsg); !ok {
		t.Errorf("expected StatusRefreshMsg after action, got %T", msg)
	}
}

func TestStatusDashboardKernelInfo(t *testing.T) {
	m := NewStatusDashboard()
	m.status = sampleStatusInfo()
	output := m.renderKernelInfo()
	if !containsStr(output, "Audit: 42") {
		t.Error("expected audit count in kernel info")
	}
}

func TestStatusDashboardRebuildTables(t *testing.T) {
	m := NewStatusDashboard()
	m.sandboxes = sampleSandboxRows()
	m.skills = sampleSkillRows()
	m.rebuildTables()

	if len(m.sandboxTable.Rows) != 3 {
		t.Errorf("expected 3 sandbox rows, got %d", len(m.sandboxTable.Rows))
	}
	if len(m.skillTable.Rows) != 2 {
		t.Errorf("expected 2 skill rows, got %d", len(m.skillTable.Rows))
	}
}

func TestStatusDashboardStartCreatedSandbox(t *testing.T) {
	m := NewStatusDashboard()
	m.sandboxes = []SandboxRow{
		{ID: "sb-003", Name: "created-vm", State: "created"},
	}
	m.rebuildTables()

	var startedID string
	m.StartSandbox = func(id string) error {
		startedID = id
		return nil
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		t.Fatal("expected start command for created sandbox")
	}
	cmd()
	if startedID != "sb-003" {
		t.Errorf("expected sb-003, got %s", startedID)
	}
}
