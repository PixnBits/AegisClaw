# chat.go — cmd/aegisclaw

## Purpose
Implements the `aegisclaw chat` TUI command — an interactive REPL for sending messages to the running daemon and reading agent responses. Supports slash commands for in-session control.

## Key Types / Functions
- `runChat(cmd, args)` — main loop: reads stdin, routes through `api.Client.Chat()`, prints agent reply.
- Slash commands handled inline:
  - `/quit` / `/exit` — end session.
  - `/safe-mode` — toggle safe mode on the daemon.
  - `/call <tool> <args>` — invoke a tool directly.
  - `/status` — print current daemon status inline.
  - `/audit` — print recent audit log entries.
  - `/shutdown` — request daemon shutdown.
- Session state is maintained by the daemon; the CLI is a stateless thin client.

## System Fit
Primary human interaction surface for the agent. All actual agent logic runs in the daemon; `chat.go` only handles TUI rendering and input routing.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/api` — daemon API client
- `golang.org/x/term` — raw terminal input
