# chat_test.go

## Purpose
Tests for `ChatModel` covering message rendering, tool execution flow, safe mode toggling, input history navigation, and the proposal status poll tick. Tests use mock callbacks to simulate SendMessage and ExecuteTool responses without requiring a running agent or sandbox.

## Key Types and Functions
- `TestChatModel_Init`: verifies `Init` returns a tick command for the proposal status poll
- `TestChatModel_SendMessage`: types a message, presses enter; verifies `SendMessage` callback is invoked with the correct content
- `TestChatModel_RenderMessages`: pre-loads messages of each role type; verifies `View` renders them with appropriate role labels
- `TestChatModel_ToolCallQueuing`: simulates a `tool_call` response during message send; verifies the tool is queued and `ExecuteTool` is called
- `TestChatModel_SafeMode_Toggle`: sends `/safe-mode` command; verifies safe mode is enabled and tool calls are blocked
- `TestChatModel_InputHistory`: sends multiple messages and verifies ↑/↓ cycles through input history
- `TestChatModel_PendingInput`: sends input during an active tool cycle; verifies it is queued and replayed after completion
- `TestChatModel_ShutdownCommand`: sends `/shutdown` and verifies `RequestShutdown` callback is invoked
- `TestChatModel_NilCallbacks`: sends messages with nil callbacks; verifies no panics

## Role in the System
Ensures the primary user-facing TUI component handles all interaction paths correctly, including edge cases like concurrent tool execution and nil callback safety.

## Dependencies
- `testing`, `github.com/charmbracelet/bubbletea`
- `internal/tui`: `ChatModel`
