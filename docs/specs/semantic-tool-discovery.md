# Semantic Tool Discovery Specification

## Overview
In addition to basic `tool.list`, agents must be able to perform **semantic search** over available tools using natural language queries. This is powered by an in-memory vector database.

## Core Commands (noun.verb)

- `tool.list` — List all available tools (exact match, fast)
- `tool.search` — Semantic / natural language search (primary method for agents)

## Implementation Details

- **Backend**: `chromem-go` (in-process vector DB with FNV-32 or better embeddings)
- **Embedding Model**: Lightweight local model (e.g. Gemma or Nomic) or simple token-based embeddings
- **Indexing**: Every deployed tool is automatically embedded when it reaches the **Deployed** state
- **Query Flow**:
  1. Agent calls `tool.search` with natural language query
  2. Query is embedded
  3. Top-K similar tools are returned (with similarity score)
  4. Results are filtered by current agent’s allowed scopes

## Example `tool.search` Response
```json
{
  "query": "I need to send a message to a Discord channel",
  "results": [
    {
      "tool": "discord_monitor.send_message",
      "skill": "discord_monitor",
      "similarity": 0.92,
      "description": "..."
    }
  ]
}
```

## Caching & Freshness
- Vector index is rebuilt when new skills are deployed
- Short in-memory cache per Agent Runtime (30–60s TTL)
- Event-driven invalidation via EventBus on skill deployment

## Related Documents
- `../skill-discovery.md`
- `../store-vm.md` (owns the canonical registry)
- `../builder-vm.md`

## Traceability
**Driven by:**
- Old `internal/lookup` package
- Need for agents to intelligently discover tools without knowing exact names
- User Journey #3 (collaborative task execution)