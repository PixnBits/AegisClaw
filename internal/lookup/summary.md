# Package: lookup

## Overview
The `lookup` package provides a persistent, semantic tool-discovery store for the AegisClaw agent. It indexes skill tools by name and description using an embedded vector database (`chromem-go`) and exposes a similarity-search API so the ReAct executor can dynamically select the most relevant tools for any given task. Embeddings are computed with a pure-Go FNV-32 hash function that produces 384-dimensional L2-normalised vectors — a self-contained approach that requires no CGO or external embedding service, with a clear upgrade path to ONNX-based models.

## Files
- `store.go`: Core `Store` implementation — indexing, lookup, and persistence via chromem-go
- `fuzz_test.go`: Fuzz tests covering the embedding function, Gemma 4 block formatter, and JSON quoting helpers
- `store_test.go`: Unit/integration tests for `NewStore`, `IndexTool`, `LookupTools`, and `Count`

## Key Abstractions
- `Store`: wraps a chromem-go collection; thread-safe handle for all operations
- `ToolEntry`: describes a skill tool with name, description, skill association, and parameters
- `FormattedTool`: a resolved tool with its Gemma 4 `<|tool|>…<|/tool|>` block, ready for LLM injection
- `hashEmbeddingFunc`: deterministic 384-dim FNV-32 embedding; normalised to unit length

## System Role
The lookup store is consumed by the ReAct FSM (`internal/runtime/exec`) during the Acting phase. Before constructing each LLM prompt, the agent queries the store to retrieve the top-N semantically matched tools and injects their Gemma 4 blocks into the context. This decouples tool selection from hard-coded lists and enables dynamic skill discovery as new tools are registered.

## Dependencies
- `github.com/philippgille/chromem-go`: embedded vector DB with persistence
- `encoding/json`: parameter and document serialisation
- Standard library: `crypto/fnv`, `math`, `context`, `os`
