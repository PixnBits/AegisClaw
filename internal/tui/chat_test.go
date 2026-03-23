package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewChatModel(t *testing.T) {
	m := NewChatModel()
	if m.view != chatViewMain {
		t.Errorf("expected initial view chatViewMain, got %d", m.view)
	}
	if len(m.messages) != 1 {
		t.Errorf("expected 1 system message, got %d", len(m.messages))
	}
	if m.messages[0].Role != ChatRoleSystem {
		t.Errorf("expected system role, got %s", m.messages[0].Role)
	}
	if m.input != "" {
		t.Errorf("expected empty input, got %q", m.input)
	}
	if m.thinking {
		t.Error("expected thinking=false")
	}
}

func TestChatInit(t *testing.T) {
	m := NewChatModel()
	cmd := m.Init()
	if cmd != nil {
		t.Error("Init should return nil")
	}
}

func TestChatWindowSize(t *testing.T) {
	m := NewChatModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(ChatModel)

	if m.width != 120 {
		t.Errorf("expected width 120, got %d", m.width)
	}
	if m.height != 40 {
		t.Errorf("expected height 40, got %d", m.height)
	}
	if m.viewHeight != 34 {
		t.Errorf("expected viewHeight 34, got %d", m.viewHeight)
	}
}

func TestChatTypeInput(t *testing.T) {
	m := NewChatModel()

	// Type "hello"
	for _, r := range "hello" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	if m.input != "hello" {
		t.Errorf("expected input %q, got %q", "hello", m.input)
	}
}

func TestChatBackspace(t *testing.T) {
	m := NewChatModel()

	for _, r := range "abc" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(ChatModel)

	if m.input != "ab" {
		t.Errorf("expected input %q after backspace, got %q", "ab", m.input)
	}
}

func TestChatBackspaceEmpty(t *testing.T) {
	m := NewChatModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(ChatModel)

	if m.input != "" {
		t.Errorf("expected empty input after backspace on empty, got %q", m.input)
	}
}

func TestChatSendMessage(t *testing.T) {
	m := NewChatModel()
	m.SendMessage = func(input string, history []ChatMessage) (ChatMessage, []ToolCall, error) {
		return ChatMessage{
			Role:      ChatRoleAssistant,
			Content:   "Response to: " + input,
			Timestamp: time.Now(),
		}, nil, nil
	}

	// Type and send
	for _, r := range "test" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(ChatModel)

	if !m.thinking {
		t.Error("expected thinking=true after send")
	}
	if m.input != "" {
		t.Errorf("expected empty input after send, got %q", m.input)
	}
	// Should have system + user messages
	if len(m.messages) != 2 {
		t.Errorf("expected 2 messages (system, user), got %d", len(m.messages))
	}

	if cmd == nil {
		t.Fatal("expected a command after send")
	}

	// Simulate the response
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(ChatModel)

	if m.thinking {
		t.Error("expected thinking=false after response")
	}
	if len(m.messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(m.messages))
	}
	if m.messages[2].Role != ChatRoleAssistant {
		t.Errorf("expected assistant role, got %s", m.messages[2].Role)
	}
	if !strings.Contains(m.messages[2].Content, "test") {
		t.Errorf("expected response referencing input, got %q", m.messages[2].Content)
	}
}

func TestChatSendEmptyDoesNothing(t *testing.T) {
	m := NewChatModel()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(ChatModel)

	if cmd != nil {
		t.Error("expected no command for empty send")
	}
	if len(m.messages) != 1 {
		t.Errorf("expected 1 message (system), got %d", len(m.messages))
	}
}

func TestChatSendWhileThinking(t *testing.T) {
	m := NewChatModel()
	m.thinking = true

	for _, r := range "msg" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(ChatModel)

	if cmd != nil {
		t.Error("expected no command while thinking")
	}
	// Input should remain since we can't send
	if m.input != "msg" {
		t.Errorf("expected input preserved, got %q", m.input)
	}
}

func TestChatSendError(t *testing.T) {
	m := NewChatModel()
	m.SendMessage = func(input string, history []ChatMessage) (ChatMessage, []ToolCall, error) {
		return ChatMessage{}, nil, fmt.Errorf("connection refused")
	}

	for _, r := range "hi" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(ChatModel)

	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(ChatModel)

	if m.err == nil {
		t.Error("expected error")
	}
	if !strings.Contains(m.err.Error(), "connection refused") {
		t.Errorf("expected error message, got %v", m.err)
	}
	if m.thinking {
		t.Error("expected thinking=false after error")
	}
}

func TestChatNilSendCallback(t *testing.T) {
	m := NewChatModel()
	// SendMessage is nil

	for _, r := range "hi" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(ChatModel)

	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(ChatModel)

	if m.thinking {
		t.Error("expected thinking=false")
	}
	// Should get fallback message
	last := m.messages[len(m.messages)-1]
	if last.Role != ChatRoleAssistant {
		t.Errorf("expected assistant role, got %s", last.Role)
	}
	if !strings.Contains(last.Content, "not configured") {
		t.Errorf("expected fallback message, got %q", last.Content)
	}
}

func TestChatToolCallExecution(t *testing.T) {
	m := NewChatModel()
	m.SendMessage = func(input string, history []ChatMessage) (ChatMessage, []ToolCall, error) {
		return ChatMessage{
			Role:      ChatRoleAssistant,
			Content:   "Let me check that.",
			Timestamp: time.Now(),
		}, []ToolCall{{Name: "list_sandboxes", Args: ""}}, nil
	}
	m.ExecuteTool = func(call ToolCall) (string, error) {
		return "sandbox-1 running\nsandbox-2 stopped", nil
	}

	for _, r := range "status" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	// Send
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(ChatModel)

	// Response with tool call
	msg := cmd()
	updated, cmd = m.Update(msg)
	m = updated.(ChatModel)

	if cmd == nil {
		t.Fatal("expected tool execution command")
	}

	// Tool result
	msg = cmd()
	updated, _ = m.Update(msg)
	m = updated.(ChatModel)

	// Should have: system, user, assistant, tool
	if len(m.messages) != 4 {
		t.Errorf("expected 4 messages, got %d", len(m.messages))
	}
	toolMsg := m.messages[3]
	if toolMsg.Role != ChatRoleTool {
		t.Errorf("expected tool role, got %s", toolMsg.Role)
	}
	if toolMsg.ToolName != "list_sandboxes" {
		t.Errorf("expected tool name list_sandboxes, got %s", toolMsg.ToolName)
	}
	if !strings.Contains(toolMsg.Content, "sandbox-1") {
		t.Errorf("expected tool content, got %q", toolMsg.Content)
	}
}

func TestChatToolCallError(t *testing.T) {
	m := NewChatModel()
	m.SendMessage = func(input string, history []ChatMessage) (ChatMessage, []ToolCall, error) {
		return ChatMessage{
			Role:      ChatRoleAssistant,
			Content:   "Checking...",
			Timestamp: time.Now(),
		}, []ToolCall{{Name: "broken_tool"}}, nil
	}
	m.ExecuteTool = func(call ToolCall) (string, error) {
		return "", fmt.Errorf("tool crashed")
	}

	for _, r := range "x" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(ChatModel)

	msg := cmd()
	updated, cmd = m.Update(msg)
	m = updated.(ChatModel)

	msg = cmd()
	updated, _ = m.Update(msg)
	m = updated.(ChatModel)

	toolMsg := m.messages[len(m.messages)-1]
	if toolMsg.Role != ChatRoleTool {
		t.Errorf("expected tool role, got %s", toolMsg.Role)
	}
	if !strings.Contains(toolMsg.Content, "error") {
		t.Errorf("expected error in tool message, got %q", toolMsg.Content)
	}
}

func TestChatNilExecuteToolCallback(t *testing.T) {
	m := NewChatModel()
	m.SendMessage = func(input string, history []ChatMessage) (ChatMessage, []ToolCall, error) {
		return ChatMessage{
			Role:      ChatRoleAssistant,
			Content:   "Running...",
			Timestamp: time.Now(),
		}, []ToolCall{{Name: "some_tool"}}, nil
	}
	// ExecuteTool is nil

	for _, r := range "go" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(ChatModel)

	msg := cmd()
	updated, cmd = m.Update(msg)
	m = updated.(ChatModel)

	msg = cmd()
	updated, _ = m.Update(msg)
	m = updated.(ChatModel)

	toolMsg := m.messages[len(m.messages)-1]
	if !strings.Contains(toolMsg.Content, "not configured") {
		t.Errorf("expected 'not configured' error, got %q", toolMsg.Content)
	}
}

func TestChatQuitCommand(t *testing.T) {
	m := NewChatModel()

	// Type /quit
	for _, r := range "/quit" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", msg)
	}
}

func TestChatExitCommand(t *testing.T) {
	m := NewChatModel()

	for _, r := range "/exit" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", msg)
	}
}

func TestChatCtrlC(t *testing.T) {
	m := NewChatModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", msg)
	}
}

func TestChatScrollUp(t *testing.T) {
	m := NewChatModel()
	m.scrollOffset = 10
	m.viewHeight = 20

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(ChatModel)

	if m.scrollOffset != 5 {
		t.Errorf("expected scrollOffset 5, got %d", m.scrollOffset)
	}
}

func TestChatScrollUpClamp(t *testing.T) {
	m := NewChatModel()
	m.scrollOffset = 2
	m.viewHeight = 20

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(ChatModel)

	if m.scrollOffset != 0 {
		t.Errorf("expected scrollOffset 0, got %d", m.scrollOffset)
	}
}

func TestChatScrollDown(t *testing.T) {
	m := NewChatModel()
	m.viewHeight = 5
	// Add enough messages to scroll
	for i := 0; i < 20; i++ {
		m.messages = append(m.messages, ChatMessage{
			Role:      ChatRoleUser,
			Content:   fmt.Sprintf("message %d", i),
			Timestamp: time.Now(),
		})
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(ChatModel)

	if m.scrollOffset != 5 {
		t.Errorf("expected scrollOffset 5, got %d", m.scrollOffset)
	}
}

func TestChatRenderView(t *testing.T) {
	m := NewChatModel()
	m.width = 80
	m.height = 24
	m.viewHeight = 18

	v := m.View()
	if !strings.Contains(v, "AegisClaw Chat") {
		t.Error("expected title in view")
	}
	if !strings.Contains(v, ">") {
		t.Error("expected prompt in view")
	}
}

func TestChatRenderWithInput(t *testing.T) {
	m := NewChatModel()
	m.width = 80
	m.height = 24
	m.viewHeight = 18
	m.input = "hello world"

	v := m.View()
	if !strings.Contains(v, "hello world") {
		t.Error("expected input in view")
	}
}

func TestChatRenderThinking(t *testing.T) {
	m := NewChatModel()
	m.width = 80
	m.height = 24
	m.viewHeight = 18
	m.thinking = true

	v := m.View()
	if !strings.Contains(v, "thinking") {
		t.Error("expected thinking indicator in view")
	}
}

func TestChatRenderError(t *testing.T) {
	m := NewChatModel()
	m.width = 80
	m.height = 24
	m.viewHeight = 18
	m.err = fmt.Errorf("something went wrong")

	v := m.View()
	if !strings.Contains(v, "something went wrong") {
		t.Error("expected error in view")
	}
}

func TestChatRenderUserMessage(t *testing.T) {
	m := NewChatModel()
	m.width = 80
	msg := ChatMessage{
		Role:      ChatRoleUser,
		Content:   "What is the status?",
		Timestamp: time.Now(),
	}
	rendered := m.renderMessage(msg)
	if !strings.Contains(rendered, "You") {
		t.Error("expected 'You' prefix for user messages")
	}
	if !strings.Contains(rendered, "What is the status?") {
		t.Error("expected content in rendered message")
	}
}

func TestChatRenderAssistantMessage(t *testing.T) {
	m := NewChatModel()
	m.width = 80
	msg := ChatMessage{
		Role:      ChatRoleAssistant,
		Content:   "Everything is running fine.",
		Timestamp: time.Now(),
	}
	rendered := m.renderMessage(msg)
	if !strings.Contains(rendered, "AegisClaw") {
		t.Error("expected 'AegisClaw' prefix for assistant messages")
	}
	if !strings.Contains(rendered, "Everything is running fine.") {
		t.Error("expected content in rendered message")
	}
}

func TestChatRenderToolMessage(t *testing.T) {
	m := NewChatModel()
	m.width = 80
	msg := ChatMessage{
		Role:     ChatRoleTool,
		Content:  "sandbox-1 running",
		ToolName: "list_sandboxes",
	}
	rendered := m.renderMessage(msg)
	if !strings.Contains(rendered, "Tool:list_sandboxes") {
		t.Error("expected tool name in rendered message")
	}
	if !strings.Contains(rendered, "sandbox-1") {
		t.Error("expected content in rendered message")
	}
}

func TestChatRenderSystemMessage(t *testing.T) {
	m := NewChatModel()
	m.width = 80
	msg := ChatMessage{
		Role:    ChatRoleSystem,
		Content: "Welcome to AegisClaw",
	}
	rendered := m.renderMessage(msg)
	if !strings.Contains(rendered, "Welcome to AegisClaw") {
		t.Error("expected content in rendered message")
	}
}

func TestChatToolResultView(t *testing.T) {
	m := NewChatModel()
	m.view = chatViewToolResult
	m.toolResult = "Detailed tool output"

	// Press Esc to go back
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(ChatModel)

	if m.view != chatViewMain {
		t.Errorf("expected chatViewMain after esc, got %d", m.view)
	}
}

func TestChatCountMessageLines(t *testing.T) {
	m := NewChatModel()
	m.width = 80
	// Has 1 system message
	lines := m.countMessageLines()
	if lines <= 0 {
		t.Errorf("expected positive line count, got %d", lines)
	}
}

func TestChatScrollToBottom(t *testing.T) {
	m := NewChatModel()
	m.viewHeight = 5
	for i := 0; i < 20; i++ {
		m.messages = append(m.messages, ChatMessage{
			Role:      ChatRoleUser,
			Content:   fmt.Sprintf("line %d", i),
			Timestamp: time.Now(),
		})
	}
	m.scrollToBottom()

	total := m.countMessageLines()
	expected := total - m.viewHeight
	if expected < 0 {
		expected = 0
	}
	if m.scrollOffset != expected {
		t.Errorf("expected scrollOffset %d, got %d", expected, m.scrollOffset)
	}
}

func TestChatMultipleToolCalls(t *testing.T) {
	callOrder := []string{}
	m := NewChatModel()
	m.SendMessage = func(input string, history []ChatMessage) (ChatMessage, []ToolCall, error) {
		return ChatMessage{
				Role:      ChatRoleAssistant,
				Content:   "Checking both...",
				Timestamp: time.Now(),
			}, []ToolCall{
				{Name: "tool_a"},
				{Name: "tool_b"},
			}, nil
	}
	m.ExecuteTool = func(call ToolCall) (string, error) {
		callOrder = append(callOrder, call.Name)
		return "ok:" + call.Name, nil
	}

	for _, r := range "go" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	// Send
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(ChatModel)

	// Response with 2 tool calls — first tool executes
	msg := cmd()
	updated, cmd = m.Update(msg)
	m = updated.(ChatModel)

	if cmd == nil {
		t.Fatal("expected tool_a execution command")
	}

	// Execute tool_a result
	msg = cmd()
	updated, _ = m.Update(msg)
	m = updated.(ChatModel)

	// Check first tool was executed
	if len(callOrder) < 1 || callOrder[0] != "tool_a" {
		t.Errorf("expected tool_a first, got %v", callOrder)
	}
}

func TestChatRenderMessagesNoHeight(t *testing.T) {
	m := NewChatModel()
	m.width = 80
	// viewHeight is 0 — should show all messages
	rendered := m.renderMessages()
	if !strings.Contains(rendered, "AegisClaw") {
		t.Error("expected system message in render when viewHeight is 0")
	}
}

func TestChatSafeModeBlocksToolExecution(t *testing.T) {
	m := NewChatModel()
	m.SafeMode = true
	m.SendMessage = func(input string, history []ChatMessage) (ChatMessage, []ToolCall, error) {
		return ChatMessage{
			Role:      ChatRoleAssistant,
			Content:   "Let me check.",
			Timestamp: time.Now(),
		}, []ToolCall{{Name: "hello-world.greet"}}, nil
	}
	m.ExecuteTool = func(call ToolCall) (string, error) {
		t.Fatal("ExecuteTool should not be called in safe mode")
		return "", nil
	}

	for _, r := range "test" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(ChatModel)

	// Get LLM response with tool call
	msg := cmd()
	updated, cmd = m.Update(msg)
	m = updated.(ChatModel)

	// Should NOT produce a follow-up tool execution command
	if cmd != nil {
		t.Error("expected no command in safe mode — tool execution should be blocked")
	}

	// Should have a system message about safe mode blocking
	last := m.messages[len(m.messages)-1]
	if last.Role != ChatRoleSystem {
		t.Errorf("expected system message about safe mode, got role=%s", last.Role)
	}
	if !strings.Contains(last.Content, "Safe mode") {
		t.Errorf("expected safe mode message, got %q", last.Content)
	}
}

func TestChatSafeModeToggle(t *testing.T) {
	m := NewChatModel()
	m.ToggleSafeMode = func(enable bool) error {
		return nil
	}

	// Type /safe-mode
	for _, r := range "/safe-mode" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(ChatModel)

	if cmd == nil {
		t.Fatal("expected safe-mode command")
	}

	// Execute the async command
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(ChatModel)

	if !m.SafeMode {
		t.Error("expected SafeMode=true after /safe-mode")
	}

	// Now disable
	for _, r := range "/safe-mode off" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(ChatModel)

	msg = cmd()
	updated, _ = m.Update(msg)
	m = updated.(ChatModel)

	if m.SafeMode {
		t.Error("expected SafeMode=false after /safe-mode off")
	}
}

func TestChatShutdownCommand(t *testing.T) {
	m := NewChatModel()
	m.RequestShutdown = func() error {
		return nil
	}

	for _, r := range "/shutdown" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(ChatModel)

	if cmd == nil {
		t.Fatal("expected shutdown command")
	}

	// Execute the async command — should produce tea.Quit
	msg := cmd()
	_, quitCmd := m.Update(msg)
	if quitCmd == nil {
		t.Fatal("expected quit command after shutdown")
	}
	quitMsg := quitCmd()
	if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg after shutdown, got %T", quitMsg)
	}
}

func TestChatSafeModeNilCallback(t *testing.T) {
	m := NewChatModel()
	// ToggleSafeMode is nil

	for _, r := range "/safe-mode" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(ChatModel)

	if cmd == nil {
		t.Fatal("expected command")
	}

	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(ChatModel)

	if m.err == nil {
		t.Error("expected error for nil callback")
	}
	if !strings.Contains(m.err.Error(), "not configured") {
		t.Errorf("expected 'not configured' error, got %v", m.err)
	}
}

func TestChatInputWrapping(t *testing.T) {
	m := NewChatModel()
	m.width = 40
	m.height = 30
	m.viewHeight = 24

	// Type a long input that exceeds the width
	longInput := "this is a very long input that should wrap around to the next line in the terminal"
	for _, r := range longInput {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(ChatModel)
	}

	view := m.View()
	// The input should be rendered with wrapping, so it should produce multiple lines
	inputSection := m.renderInput()
	lines := strings.Split(inputSection, "\n")
	// With width=40 and a ~85-char input, we expect wrapping to produce multiple lines
	if len(lines) < 2 {
		t.Errorf("expected input to wrap into multiple lines, got %d lines", len(lines))
	}
	_ = view
}

func TestChatScrollIndicators(t *testing.T) {
	m := NewChatModel()
	m.width = 80
	m.height = 20
	m.viewHeight = 10

	// Add enough messages to overflow the viewport
	for i := 0; i < 30; i++ {
		m.messages = append(m.messages, ChatMessage{
			Role:      ChatRoleAssistant,
			Content:   fmt.Sprintf("Message %d with some content", i),
			Timestamp: time.Now(),
		})
	}
	m.scrollToBottom()

	rendered := m.renderMessages()
	// At the bottom, there should be an "up" indicator but no "down" indicator
	if !strings.Contains(rendered, "\u2191 more") {
		t.Error("expected up-scroll indicator when scrolled to bottom")
	}

	// Scroll to the top
	m.scrollOffset = 0
	rendered = m.renderMessages()
	// At the top, there should be a "down" indicator but no "up" indicator
	if !strings.Contains(rendered, "\u2193 more") {
		t.Error("expected down-scroll indicator when scrolled to top")
	}
	if strings.Contains(rendered, "\u2191 more") {
		t.Error("should not show up-scroll indicator when at top")
	}
}

func TestChatProposalStatusNotification(t *testing.T) {
	m := NewChatModel()
	m.width = 80
	m.height = 30
	m.viewHeight = 24

	// Simulate receiving a proposal notification
	updated, cmd := m.Update(ChatProposalNotifyMsg{
		ProposalID: "abc12345-6789-0000-0000-000000000000",
		Title:      "My Skill",
		OldStatus:  "submitted",
		NewStatus:  "approved",
	})
	m = updated.(ChatModel)

	// Should have added a system message
	found := false
	for _, msg := range m.messages {
		if msg.Role == ChatRoleSystem && strings.Contains(msg.Content, "approved") && strings.Contains(msg.Content, "My Skill") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected system message with status change notification")
	}
	// Terminal status should not schedule more polling
	if cmd != nil {
		t.Error("expected no further poll command for terminal status with no watched proposals")
	}
}

func TestChatProposalWatchAfterSubmit(t *testing.T) {
	m := NewChatModel()
	m.width = 80
	m.height = 30
	m.viewHeight = 24

	// Simulate a successful proposal.submit tool result
	updated, cmd := m.Update(ChatToolResultMsg{
		Call:   ToolCall{Name: "proposal.submit", Args: "{}"},
		Result: "Proposal submitted.\n  ID: test-1234-5678\n  Status: submitted",
	})
	m = updated.(ChatModel)

	// Should have started watching
	if m.watchedProposals == nil || m.watchedProposals["test-1234-5678"] != "submitted" {
		t.Errorf("expected proposal to be watched, got %v", m.watchedProposals)
	}
	// Should have a poll command (from tea.Tick batch or direct)
	if cmd == nil {
		t.Error("expected poll command after proposal submission")
	}
}

func TestExtractProposalID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Proposal submitted.\n  ID: abc-123\n  Status: submitted", "abc-123"},
		{"No id here", ""},
		{"ID: simple-id", "simple-id"},
	}
	for _, tt := range tests {
		got := extractProposalID(tt.input)
		if got != tt.expected {
			t.Errorf("extractProposalID(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIsTerminalProposalStatus(t *testing.T) {
	for _, s := range []string{"approved", "rejected", "withdrawn", "complete", "failed"} {
		if !isTerminalProposalStatus(s) {
			t.Errorf("expected %q to be terminal", s)
		}
	}
	for _, s := range []string{"draft", "submitted", "in_review", "implementing"} {
		if isTerminalProposalStatus(s) {
			t.Errorf("expected %q to NOT be terminal", s)
		}
	}
}
