# store.go

## Purpose
Implements a persistent semantic tool-lookup store using the `chromem-go` embedded vector database. It indexes skill tools by name and description and enables fuzzy semantic search via a pure-Go FNV-32 hash-based embedding function (384 dimensions, no CGO). Vectors are L2-normalised and formatted to match Gemma 4 tool block syntax. The default persistence directory is `/data/vectordb` and all data is stored in a collection named "skills".

## Key Types and Functions
- `Store` struct: central handle to the chromem-go collection
- `NewStore(dir string) (*Store, error)`: opens or creates the persistent vector DB
- `IndexTool(ctx, ToolEntry) error`: upserts a tool into the vector store
- `LookupTools(ctx, query string, topN int) ([]FormattedTool, error)`: semantic search returning top-N matches
- `Count(ctx) (int, error)`: returns the number of indexed tools
- `ToolEntry`: describes a skill tool (Name, Description, SkillName, Parameters)
- `FormattedTool`: Name plus a Gemma 4 `<|tool|>…<|/tool|>` block
- `hashEmbeddingFunc`: pure-Go FNV-32 deterministic embedding (placeholder for ONNX upgrade)

## Role in the System
Acts as the dynamic tool-discovery layer for the ReAct agent. When the agent needs to select tools for a task, it queries this store to find the most semantically relevant skill tools, enabling LLM-agnostic tool routing without a remote embedding service.

## Dependencies
- `github.com/philippgille/chromem-go`: embedded vector DB
- `encoding/json`: parameter serialisation
- Standard library: `crypto/fnv`, `math`, `context`
