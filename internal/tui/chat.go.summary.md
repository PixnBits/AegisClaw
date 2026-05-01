# chat.go

## Purpose
Implements the main ReAct chat interface — the primary user-facing TUI model for interacting with the AegisClaw agent. The chat model manages the full conversation lifecycle: displaying user and assistant messages, queuing tool calls, serialising tool execution, and rendering tool results. A safe mode flag can block tool execution for cautious review. The model also polls for proposal status updates every 5 seconds.

## Key Types and Functions
- `ChatModel`: bubbletea `Model`; holds messages (user/assistant/tool/system), pending input queue, tool call queue, and safe mode flag
- `Init() tea.Cmd`: starts the proposal status poll tick
- `Update(tea.Msg) (tea.Model, tea.Cmd)`: handles keyboard input, tool results, proposal status updates, and tick messages
- `View() string`: renders the conversation history with role-coloured messages and a text input box
- Input history: ↑/↓ arrow keys navigate previous inputs
- Pending queue: inputs received during an active tool cycle are queued and replayed after the cycle completes
- Safe mode: `/safe-mode` command toggles; blocks tool calls when active
- Commands: `/safe-mode` (toggle safe mode), `/shutdown` (request daemon shutdown)
- Callbacks: `SendMessage`, `ExecuteTool`, `SummarizeToolResult`, `ToggleSafeMode`, `RequestShutdown`, `CheckProposalStatus`

## Role in the System
The chat view is the default screen users see when running `aegisclaw chat`. It drives the entire interactive agent experience, from typing a task to seeing the agent reason, call tools, and produce a final answer.

## Dependencies
- `github.com/charmbracelet/bubbletea`: model/update/view lifecycle
- `time`: 5-second proposal status poll tick
- `internal/tui`: shared styles and components
