# Memory VM Specification

**Status:** Draft  
**Last Updated:** May 2026

## Purpose

The Memory VM manages both short-term conversation context and long-term memory for exactly one agent. It is the single source of truth for that agent’s state.

There is a 1:1 relationship between an Agent Runtime VM and a Memory VM.

## Communication Interface

The Agent Runtime VM communicates with its Memory VM using these **explicit commands**:

### 1. `memory.get_context`
- **Purpose**: Called at the start of every agent turn
- **Returns**: 
  - Recent short-term conversation history
  - Top N most relevant long-term memories (via semantic search)
  - Current token usage summary

### 2. `memory.store`
- **Purpose**: Agent explicitly wants to save information
- **Payload**: `content`, `importance` (optional), `tags` (optional)
- Stores the memory as long-term

### 3. `memory.search`
- **Purpose**: Agent wants to query long-term memory
- **Payload**: `query` (natural language), `limit` (default 5)
- Returns semantically relevant memories

### 4. `memory.summarize`
- **Purpose**: Trigger manual summarization of current short-term context

## Key Implementation Decisions

- **Short-term Context**: Hard limit of **32,000 tokens**. Automatically summarizes oldest content when approaching limit.
- **Long-term Retrieval**: Always uses semantic search. Never dumps all memories.
- **Default Behavior**: `memory.get_context` is called automatically before every agent turn.
- **Embedding Model**: `nomic-embed-text` (or equivalent small, fast model)
- **Persistence**: All long-term memories are immediately written to the Store VM.

## Test Requirements

- `memory.get_context` must return both recent history + relevant long-term memories
- Semantic search must return relevant results for natural language queries
- Short-term context must never exceed 32k tokens
- Agent Runtime VM crash + restart must not lose conversation state
- One agent must not be able to read another agent’s memories

## Traceability

**Driven by:**
- [../prd/runtime-architecture.md](../prd/runtime-architecture.md) — Sandbox separation and Agent Runtime requirements
- [../prd/security-model.md](../prd/security-model.md) — Isolation and state protection guarantees

**See also:**
- [../architecture.md](../architecture.md)
- [../prd/glossary.md](../prd/glossary.md)