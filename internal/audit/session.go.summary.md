# `session.go` — Chat Session Audit Log

## Purpose
Provides per-conversation JSONL audit logging for the AegisClaw chat subsystem (D2). Each `SessionLog` records ordered events — user messages, assistant replies, tool calls, slash commands, and session lifecycle markers — to a timestamped file under `<audit-dir>/chat-sessions/`.

## Key Types / Functions

| Symbol | Description |
|--------|-------------|
| `SessionEventType` | String enum: `session_start`, `session_end`, `user_message`, `assistant_message`, `tool_call`, `tool_result`, `slash_command`, `system_message`. |
| `SessionEvent` | Single log entry with `Timestamp`, `SessionID`, `Event`, `Role`, `Content`, `ToolName`, `ToolArgs`, `Error`. |
| `SessionLog` | Wraps an `*os.File` (JSONL) and a mutex; auto-fills `Timestamp` and `SessionID` on every write. |
| `NewSessionLog(dir)` | Creates the `chat-sessions/` subdirectory (mode `0700`), generates a UUID session ID, names the file `<timestamp>_<id[:8]>.jsonl` (mode `0600`), and writes an opening `session_start` event. |
| `SessionLog.Log(evt)` | Best-effort write + fsync; silently drops errors to avoid crashing the chat on I/O failures. |
| `SessionLog.Close()` | Writes `session_end` then closes the file. |

## How It Fits Into the Broader System
`SessionLog` is created by the daemon's chat handler for each new conversation and passed to the agent loop so every LLM turn, tool execution, and slash command is durably recorded for audit and replay.

## Notable Dependencies
- `github.com/google/uuid` for session IDs.
- Standard library `os`, `sync`, `encoding/json`, `time`.
