# trace_context.go — cmd/aegisclaw

## Purpose
Provides context-key helpers for propagating ReAct trace IDs through the request context without leaking the key type to other packages.

## Key Types / Functions
- `reActTraceKey` — unexported struct used as the context key (prevents collisions).
- `withReActTraceID(ctx, id)` — stores a trace ID in the context.
- `reActTraceIDFromContext(ctx)` — retrieves the trace ID; returns `""` if absent.

## System Fit
Used by `chat_handlers.go` to attach a trace ID to every agent turn so that all tool calls within a single chat turn share the same ID in the audit log.

## Notable Dependencies
- Standard library only (`context`).
