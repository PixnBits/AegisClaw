package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// chatView tracks the current chat state.
type chatView int

const (
	chatViewMain chatView = iota
	chatViewToolResult
)

// ChatRole identifies the sender of a message.
type ChatRole string

const (
	ChatRoleUser      ChatRole = "user"
	ChatRoleAssistant ChatRole = "assistant"
	ChatRoleTool      ChatRole = "tool"
	ChatRoleSystem    ChatRole = "system"
)

// ChatMessage represents a single message in the chat.
type ChatMessage struct {
	Role      ChatRole
	Content   string
	Timestamp time.Time
	ToolName  string

	// OriginalContent preserves the full LLM output including tool-call
	// blocks. The View uses Content (cleaned for display) while history
	// building uses OriginalContent so the LLM sees its own prior tool
	// calls. Empty means Content is the original.
	OriginalContent string
}

// ToolCall represents a tool invocation from the assistant.
type ToolCall struct {
	Name   string
	Args   string
	Result string
}

// ChatModel is the bubbletea Model for the ReAct chat interface.
type ChatModel struct {
	messages     []ChatMessage
	input        string
	scrollOffset int
	viewHeight   int
	width        int
	height       int
	keys         KeyMap
	view         chatView
	toolResult   string
	thinking     bool
	err          error

	// Input history (most recent last).
	inputHistory []string
	historyIndex int    // -1 = not browsing; 0..len-1 = browsing
	savedInput   string // stash current input when browsing

	// Safe mode blocks all tool and skill execution.
	SafeMode bool

	// Watched proposals for status change notifications.
	watchedProposals map[string]string // proposal ID → last known status

	// Callbacks
	SendMessage         func(input string, history []ChatMessage) (ChatMessage, []ToolCall, error)
	ExecuteTool         func(call ToolCall) (string, error)
	SummarizeToolResult func(toolName, toolResult string, history []ChatMessage) (ChatMessage, error)
	ToggleSafeMode      func(enable bool) error
	RequestShutdown     func() error
	CheckProposalStatus func(id string) (status, title string, err error)
}

// ChatSafeModeMsg carries the result of a safe-mode toggle.
type ChatSafeModeMsg struct {
	Enabled bool
	Err     error
}

// ChatShutdownMsg signals the TUI to exit after a shutdown request.
type ChatShutdownMsg struct {
	Err error
}

// ChatResponseMsg carries the assistant response.
type ChatResponseMsg struct {
	Message   ChatMessage
	ToolCalls []ToolCall
	Err       error
}

// ChatToolResultMsg carries the result of a tool execution.
type ChatToolResultMsg struct {
	Call      ToolCall
	Result    string
	Err       error
	Remaining []ToolCall
}

// chatPollTickMsg triggers a proposal status check after a delay.
type chatPollTickMsg struct{}

// chatPollNoChangeMsg re-arms the poll timer when no changes were found.
type chatPollNoChangeMsg struct{}

// ChatProposalNotifyMsg delivers a proposal status change notification.
type ChatProposalNotifyMsg struct {
	ProposalID string
	Title      string
	OldStatus  string
	NewStatus  string
}

// NewChatModel creates a new chat model.
func NewChatModel() ChatModel {
	return ChatModel{
		messages: []ChatMessage{
			{
				Role:      ChatRoleSystem,
				Content:   "AegisClaw ReAct Chat — Type a message or /help for commands. ↑/↓ to recall history.",
				Timestamp: time.Now(),
			},
		},
		keys:         DefaultKeyMap(),
		view:         chatViewMain,
		historyIndex: -1,
	}
}

// Init returns nil (no initial command).
func (m ChatModel) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m ChatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewHeight = msg.Height - 6
		return m, nil

	case ChatSafeModeMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.SafeMode = msg.Enabled
		label := "ENABLED"
		if !msg.Enabled {
			label = "DISABLED"
		}
		m.messages = append(m.messages, ChatMessage{
			Role:      ChatRoleSystem,
			Content:   fmt.Sprintf("Safe mode %s.", label),
			Timestamp: time.Now(),
		})
		m.scrollToBottom()
		return m, nil

	case ChatShutdownMsg:
		if msg.Err != nil {
			m.err = msg.Err
		}
		return m, tea.Quit

	case ChatResponseMsg:
		m.thinking = false
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.messages = append(m.messages, msg.Message)
		m.scrollToBottom()

		// Execute tool calls sequentially (blocked in safe mode)
		if len(msg.ToolCalls) > 0 {
			if m.SafeMode {
				m.messages = append(m.messages, ChatMessage{
					Role:      ChatRoleSystem,
					Content:   "Safe mode: tool execution blocked.",
					Timestamp: time.Now(),
				})
				m.scrollToBottom()
				return m, nil
			}
			return m, m.executeTool(msg.ToolCalls[0], msg.ToolCalls[1:])
		}
		return m, nil

	case ChatToolResultMsg:
		toolContent := msg.Result
		if msg.Err != nil {
			toolContent = fmt.Sprintf("Tool %s error: %v", msg.Call.Name, msg.Err)
		}
		m.messages = append(m.messages, ChatMessage{
			Role:      ChatRoleTool,
			Content:   toolContent,
			Timestamp: time.Now(),
			ToolName:  msg.Call.Name,
		})
		m.scrollToBottom()

		// Start watching proposal status after successful submission.
		var watchCmd tea.Cmd
		if msg.Call.Name == "proposal.submit" && msg.Err == nil {
			if id := extractProposalID(msg.Result); id != "" {
				if m.watchedProposals == nil {
					m.watchedProposals = make(map[string]string)
				}
				m.watchedProposals[id] = "submitted"
				watchCmd = m.pollProposals()
			}
		}

		// Chain remaining tool calls, or summarize if all done.
		if len(msg.Remaining) > 0 {
			nextCmd := m.executeTool(msg.Remaining[0], msg.Remaining[1:])
			if watchCmd != nil {
				return m, tea.Batch(nextCmd, watchCmd)
			}
			return m, nextCmd
		}
		if m.SummarizeToolResult != nil {
			m.thinking = true
			nextCmd := m.summarizeResult(msg.Call.Name, toolContent)
			if watchCmd != nil {
				return m, tea.Batch(nextCmd, watchCmd)
			}
			return m, nextCmd
		}
		if watchCmd != nil {
			return m, watchCmd
		}
		return m, nil

	case chatPollTickMsg:
		if len(m.watchedProposals) > 0 {
			return m, m.checkWatchedProposals()
		}
		return m, nil

	case chatPollNoChangeMsg:
		if len(m.watchedProposals) > 0 {
			return m, m.pollProposals()
		}
		return m, nil

	case ChatProposalNotifyMsg:
		if m.watchedProposals == nil {
			m.watchedProposals = make(map[string]string)
		}
		m.watchedProposals[msg.ProposalID] = msg.NewStatus
		m.messages = append(m.messages, ChatMessage{
			Role:      ChatRoleSystem,
			Content:   fmt.Sprintf("Proposal %s (%s): status changed to %s", truncateID(msg.ProposalID, 8), msg.Title, msg.NewStatus),
			Timestamp: time.Now(),
		})
		m.scrollToBottom()
		if isTerminalProposalStatus(msg.NewStatus) {
			delete(m.watchedProposals, msg.ProposalID)
		}
		if len(m.watchedProposals) > 0 {
			return m, m.pollProposals()
		}
		return m, nil

	case ChatSummaryMsg:
		m.thinking = false
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.messages = append(m.messages, msg.Message)
		m.scrollToBottom()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m ChatModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.view {
	case chatViewMain:
		return m.handleMainKey(msg)
	case chatViewToolResult:
		if key.Matches(msg, m.keys.Back) {
			m.view = chatViewMain
			return m, nil
		}
		return m, nil
	}
	return m, nil
}

func (m ChatModel) handleMainKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit

	case tea.KeyEnter:
		if m.input == "" || m.thinking {
			return m, nil
		}
		input := m.input
		m.input = ""
		m.historyIndex = -1

		// Record in history (deduplicate consecutive repeats).
		if len(m.inputHistory) == 0 || m.inputHistory[len(m.inputHistory)-1] != input {
			m.inputHistory = append(m.inputHistory, input)
		}

		// Check for quit commands
		if input == "/quit" || input == "/exit" {
			return m, tea.Quit
		}

		// /safe-mode and /shutdown are handled directly without LLM.
		if input == "/safe-mode" || input == "/safe-mode on" {
			m.messages = append(m.messages, ChatMessage{
				Role: ChatRoleUser, Content: input, Timestamp: time.Now(),
			})
			m.scrollToBottom()
			return m, m.toggleSafeMode(true)
		}
		if input == "/safe-mode off" {
			m.messages = append(m.messages, ChatMessage{
				Role: ChatRoleUser, Content: input, Timestamp: time.Now(),
			})
			m.scrollToBottom()
			return m, m.toggleSafeMode(false)
		}
		if input == "/shutdown" {
			m.messages = append(m.messages, ChatMessage{
				Role: ChatRoleUser, Content: input, Timestamp: time.Now(),
			})
			m.messages = append(m.messages, ChatMessage{
				Role: ChatRoleSystem, Content: "Shutting down all skills and the daemon...", Timestamp: time.Now(),
			})
			m.scrollToBottom()
			return m, m.requestShutdown()
		}

		// Add user message
		m.messages = append(m.messages, ChatMessage{
			Role:      ChatRoleUser,
			Content:   input,
			Timestamp: time.Now(),
		})
		m.scrollToBottom()
		m.thinking = true
		return m, m.sendMessage(input)

	case tea.KeyBackspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return m, nil

	case tea.KeySpace:
		m.input += " "
		return m, nil

	case tea.KeyUp:
		if len(m.inputHistory) == 0 {
			return m, nil
		}
		if m.historyIndex == -1 {
			// Start browsing: save current input, jump to most recent.
			m.savedInput = m.input
			m.historyIndex = len(m.inputHistory) - 1
		} else if m.historyIndex > 0 {
			m.historyIndex--
		}
		m.input = m.inputHistory[m.historyIndex]
		return m, nil

	case tea.KeyDown:
		if m.historyIndex == -1 {
			return m, nil
		}
		if m.historyIndex < len(m.inputHistory)-1 {
			m.historyIndex++
			m.input = m.inputHistory[m.historyIndex]
		} else {
			// Past the end: restore saved input.
			m.historyIndex = -1
			m.input = m.savedInput
		}
		return m, nil

	case tea.KeyRunes:
		m.input += string(msg.Runes)
		m.historyIndex = -1
		return m, nil

	default:
		switch {
		case key.Matches(msg, m.keys.PageUp):
			m.scrollOffset -= 5
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
			return m, nil
		case key.Matches(msg, m.keys.PageDown):
			m.scrollOffset += 5
			totalLines := m.countMessageLines()
			maxOffset := totalLines - m.viewHeight
			if maxOffset < 0 {
				maxOffset = 0
			}
			if m.scrollOffset > maxOffset {
				m.scrollOffset = maxOffset
			}
			return m, nil
		}
	}

	return m, nil
}

// View renders the chat interface.
func (m ChatModel) View() string {
	var b strings.Builder

	title := TitleStyle.Render("AegisClaw Chat")
	b.WriteString(title)
	b.WriteString("\n")

	if m.err != nil {
		b.WriteString(ErrorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n")
	}

	// Message area
	b.WriteString(m.renderMessages())

	// Thinking indicator
	if m.thinking {
		b.WriteString(StatusPending.Render("  thinking..."))
		b.WriteString("\n")
	}

	// Input line
	b.WriteString(m.renderInput())

	return b.String()
}

func (m ChatModel) renderMessages() string {
	var lines []string

	for _, msg := range m.messages {
		rendered := m.renderMessage(msg)
		msgLines := strings.Split(rendered, "\n")
		lines = append(lines, msgLines...)
	}

	// Apply scroll offset and height limit
	start := m.scrollOffset
	if start >= len(lines) {
		start = 0
	}
	end := start + m.viewHeight
	if end > len(lines) {
		end = len(lines)
	}
	if m.viewHeight <= 0 {
		end = len(lines)
	}

	visible := lines[start:end]
	result := strings.Join(visible, "\n") + "\n"

	// Show scroll indicators when content extends outside the viewport.
	if start > 0 {
		result = MutedStyle.Render("  \u2191 more (PgUp)") + "\n" + result
	}
	if end < len(lines) {
		result += MutedStyle.Render("  \u2193 more (PgDn)") + "\n"
	}

	return result
}

func (m ChatModel) renderMessage(msg ChatMessage) string {
	maxWidth := m.width - 6
	if maxWidth < 20 {
		maxWidth = 80
	}

	switch msg.Role {
	case ChatRoleUser:
		prefix := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render("You")
		ts := MutedStyle.Render(msg.Timestamp.Format("15:04"))
		content := lipgloss.NewStyle().Width(maxWidth).Render(msg.Content)
		return fmt.Sprintf("  %s %s\n  %s", prefix, ts, content)

	case ChatRoleAssistant:
		prefix := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent).Render("AegisClaw")
		ts := MutedStyle.Render(msg.Timestamp.Format("15:04"))
		content := lipgloss.NewStyle().Width(maxWidth).Render(msg.Content)
		return fmt.Sprintf("  %s %s\n  %s", prefix, ts, content)

	case ChatRoleTool:
		prefix := lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Render(fmt.Sprintf("Tool:%s", msg.ToolName))
		content := lipgloss.NewStyle().Width(maxWidth).Foreground(ColorMuted).Render(msg.Content)
		return fmt.Sprintf("  %s\n  %s", prefix, content)

	case ChatRoleSystem:
		return MutedStyle.Render("  " + msg.Content)
	}

	return msg.Content
}

func (m ChatModel) renderInput() string {
	prompt := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render("> ")
	cursor := lipgloss.NewStyle().Foreground(ColorAccent).Render("_")

	maxW := m.width - 2
	if maxW < 20 {
		maxW = 80
	}

	line := prompt + m.input + cursor
	return "\n" + lipgloss.NewStyle().Width(maxW).Render(line)
}

func (m *ChatModel) scrollToBottom() {
	totalLines := m.countMessageLines()
	maxOffset := totalLines - m.viewHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	m.scrollOffset = maxOffset
}

func (m ChatModel) countMessageLines() int {
	count := 0
	for _, msg := range m.messages {
		rendered := m.renderMessage(msg)
		count += strings.Count(rendered, "\n") + 1
	}
	return count
}

func (m ChatModel) sendMessage(input string) tea.Cmd {
	return func() tea.Msg {
		if m.SendMessage == nil {
			return ChatResponseMsg{
				Message: ChatMessage{
					Role:      ChatRoleAssistant,
					Content:   "Chat backend not configured. Available commands: /propose, /status, /audit, /court, /quit",
					Timestamp: time.Now(),
				},
			}
		}
		msg, tools, err := m.SendMessage(input, m.messages)
		if err != nil {
			return ChatResponseMsg{Err: err}
		}
		return ChatResponseMsg{Message: msg, ToolCalls: tools}
	}
}

func (m ChatModel) executeTool(call ToolCall, remaining []ToolCall) tea.Cmd {
	return func() tea.Msg {
		if m.ExecuteTool == nil {
			return ChatToolResultMsg{Call: call, Err: fmt.Errorf("tool execution not configured"), Remaining: remaining}
		}
		result, err := m.ExecuteTool(call)
		return ChatToolResultMsg{Call: call, Result: result, Err: err, Remaining: remaining}
	}
}

// ChatSummaryMsg carries the LLM's summary of a tool result.
type ChatSummaryMsg struct {
	Message ChatMessage
	Err     error
}

func (m ChatModel) summarizeResult(toolName, toolResult string) tea.Cmd {
	return func() tea.Msg {
		msg, err := m.SummarizeToolResult(toolName, toolResult, m.messages)
		return ChatSummaryMsg{Message: msg, Err: err}
	}
}

func (m ChatModel) toggleSafeMode(enable bool) tea.Cmd {
	return func() tea.Msg {
		if m.ToggleSafeMode == nil {
			return ChatSafeModeMsg{Enabled: enable, Err: fmt.Errorf("safe-mode handler not configured")}
		}
		err := m.ToggleSafeMode(enable)
		if err != nil {
			return ChatSafeModeMsg{Err: err}
		}
		return ChatSafeModeMsg{Enabled: enable}
	}
}

func (m ChatModel) requestShutdown() tea.Cmd {
	return func() tea.Msg {
		if m.RequestShutdown == nil {
			return ChatShutdownMsg{Err: fmt.Errorf("shutdown handler not configured")}
		}
		err := m.RequestShutdown()
		return ChatShutdownMsg{Err: err}
	}
}

func (m ChatModel) pollProposals() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return chatPollTickMsg{}
	})
}

func (m ChatModel) checkWatchedProposals() tea.Cmd {
	return func() tea.Msg {
		if m.CheckProposalStatus == nil {
			return chatPollNoChangeMsg{}
		}
		for id, lastStatus := range m.watchedProposals {
			status, title, err := m.CheckProposalStatus(id)
			if err != nil || status == lastStatus {
				continue
			}
			return ChatProposalNotifyMsg{
				ProposalID: id,
				Title:      title,
				OldStatus:  lastStatus,
				NewStatus:  status,
			}
		}
		return chatPollNoChangeMsg{}
	}
}

// extractProposalID parses a proposal ID from a tool result string.
func extractProposalID(result string) string {
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ID:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "ID:"))
		}
	}
	return ""
}

func truncateID(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func isTerminalProposalStatus(status string) bool {
	switch status {
	case "approved", "rejected", "withdrawn", "complete", "failed":
		return true
	}
	return false
}
