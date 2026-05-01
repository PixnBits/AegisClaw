# ollama.go

## Purpose
Defines the HTTP client for communicating with a local Ollama instance. Provides all request/response types for the Ollama REST API and the `Client` struct that wraps them.

## Key Types / Functions
- **`GenerateRequest` / `GenerateResponse`** – payloads for `/api/generate` (completion-style inference).
- **`ChatMessage` / `ChatRequest` / `ChatResponse`** – payloads for `/api/chat` (conversational inference with role-based message history, thinking field support).
- **`ModelInfo` / `ListResponse`** – model list from `/api/tags`.
- **`ShowRequest` / `ShowResponse` / `ModelDetails`** – detailed metadata from `/api/show`.
- **`PullRequest` / `PullResponse`** – model download via `/api/pull`.
- **`ClientConfig`** – endpoint URL, HTTP timeout, and optional custom `*http.Client` test seam.
- **`Client`** – wraps `*http.Client`; all calls are non-streaming.
- **`Client.Generate`** / **`Client.Chat`** / **`Client.List`** / **`Client.Show`** / **`Client.Pull`** – typed API methods.
- **`Client.Healthy(ctx)`** – liveness check via GET `/`.

## System Role
Foundational transport layer consumed by `Enforcer`, `Manager`, and `Verifier`. Keeping Ollama communication centralised here means only one place needs updating when the Ollama API changes.

## Notable Dependencies
- `net/http` – HTTP transport.
- `encoding/json` – request/response serialisation.
- `context` – per-call cancellation and deadlines.
