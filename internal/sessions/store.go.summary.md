# store.go

## Purpose
Implements an in-memory-only, ephemeral session registry for tracking active agent chat sessions. Sessions are never persisted to disk — they exist only for the lifetime of the daemon process. Each session holds a conversation history capped at 200 messages, and the store itself is capped at 100 sessions. When the capacity limit is reached, the oldest idle or closed session is evicted to make room for new ones.

## Key Types and Functions
- `Status`: typed string — `StatusActive`, `StatusIdle`, `StatusClosed`
- `Message`: Role, Content, Timestamp fields
- `Record`: ID, SandboxID, StartedAt, LastActiveAt, Status, plus unexported `messages` slice
- `Store`: thread-safe session registry with `sync.RWMutex`
- `NewStore() *Store`: creates an empty store
- `Open(id, sandboxID string) (*Record, error)`: idempotent open; re-activates closed sessions; auto-generates UUID if id is empty
- `Get(id string) (*Record, error)`: returns a shallow copy of the record
- `List() []*Record`: returns shallow copies of all records
- `AppendMessage(id, role, content string) error`: auto-creates session if absent; updates `LastActiveAt`
- `SetStatus(id string, status Status) error`: explicit status transition
- `Close(id string) error`: marks session closed
- `History(id string, limit int) ([]Message, error)`: returns capped message slice (most recent `limit` messages)
- `GenerateID() string`: returns a new UUID v4

## Role in the System
The session store is the conversation management layer for the TUI chat interface and the agent orchestration loop. It enables the chat model and tool execution pipeline to append messages and track session lifecycle without disk I/O.

## Dependencies
- `github.com/google/uuid`: session ID generation
- `sync`, `time`: concurrency and timestamps
