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

	// Callbacks
	SendMessage func(input string, history []ChatMessage) (ChatMessage, []ToolCall, error)
	ExecuteTool func(call ToolCall) (string, error)
}

// ChatResponseMsg carries the assistant response.
type ChatResponseMsg struct {
	Message   ChatMessage
	ToolCalls []ToolCall
	Err       error
}

// ChatToolResultMsg carries the result of a tool execution.
type ChatToolResultMsg struct {
	Call   ToolCall
	Result string
	Err    error
}

// NewChatModel creates a new chat model.
func NewChatModel() ChatModel {
	return ChatModel{
		messages: []ChatMessage{
			{
				Role:      ChatRoleSystem,
				Content:   "AegisClaw ReAct Chat — Type a message to interact with the system. Use /propose, /status, /audit, /court as shortcuts.",
				Timestamp: time.Now(),
			},
		},
		keys: DefaultKeyMap(),
		view: chatViewMain,
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

	case ChatResponseMsg:
		m.thinking = false
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		m.messages = append(m.messages, msg.Message)
		m.scrollToBottom()

		// Execute tool calls sequentially
		if len(msg.ToolCalls) > 0 {
			return m, m.executeTool(msg.ToolCalls[0], msg.ToolCalls[1:])
		}
		return m, nil

	case ChatToolResultMsg:
		if msg.Err != nil {
			m.messages = append(m.messages, ChatMessage{
				Role:      ChatRoleTool,
				Content:   fmt.Sprintf("Tool %s error: %v", msg.Call.Name, msg.Err),
				Timestamp: time.Now(),
				ToolName:  msg.Call.Name,
			})
		} else {
			m.messages = append(m.messages, ChatMessage{
				Role:      ChatRoleTool,
				Content:   msg.Result,
				Timestamp: time.Now(),
				ToolName:  msg.Call.Name,
			})
		}
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

		// Check for quit commands
		if input == "/quit" || input == "/exit" {
			return m, tea.Quit
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

	case tea.KeyRunes:
		m.input += string(msg.Runes)
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
	return strings.Join(visible, "\n") + "\n"
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
		content := lipgloss.NewStyle().MaxWidth(maxWidth).Render(msg.Content)
		return fmt.Sprintf("  %s %s\n  %s", prefix, ts, content)

	case ChatRoleAssistant:
		prefix := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent).Render("AegisClaw")
		ts := MutedStyle.Render(msg.Timestamp.Format("15:04"))
		content := lipgloss.NewStyle().MaxWidth(maxWidth).Render(msg.Content)
		return fmt.Sprintf("  %s %s\n  %s", prefix, ts, content)

	case ChatRoleTool:
		prefix := lipgloss.NewStyle().Bold(true).Foreground(ColorWarning).Render(fmt.Sprintf("Tool:%s", msg.ToolName))
		content := lipgloss.NewStyle().MaxWidth(maxWidth).Foreground(ColorMuted).Render(msg.Content)
		return fmt.Sprintf("  %s\n  %s", prefix, content)

	case ChatRoleSystem:
		return MutedStyle.Render("  " + msg.Content)
	}

	return msg.Content
}

func (m ChatModel) renderInput() string {
	prompt := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Render("> ")
	cursor := lipgloss.NewStyle().Foreground(ColorAccent).Render("_")
	line := prompt + m.input + cursor
	return "\n" + line
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
			return ChatToolResultMsg{Call: call, Err: fmt.Errorf("tool execution not configured")}
		}
		result, err := m.ExecuteTool(call)
		return ChatToolResultMsg{Call: call, Result: result, Err: err}
	}
}
