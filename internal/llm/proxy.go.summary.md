# proxy.go

## Purpose
Implements `OllamaProxy`, the host-side LLM inference proxy that accepts connections from Firecracker skill VMs over vsock Unix domain sockets and forwards inference requests to the local Ollama service. This is the primary security boundary between sandboxed skill VMs and the host LLM.

## Key Types / Functions
- **`ProxyRequest`** / **`ProxyResponse`** – JSON wire types for the guest↔host vsock protocol.
- **`ProxyToolCall`** / **`ProxyToolFunction`** – mirrors Ollama's native tool-call shape for propagation over vsock.
- **`ChatProgressSnapshot`** – in-memory streaming progress record per `stream_id`.
- **`GetChatProgress(streamID)`** – public accessor for streaming progress; snapshots expire after 15 minutes.
- **`OllamaProxy`** – core struct; holds model allowlist, HTTP client, kernel reference, and per-VM listeners.
- **`NewOllamaProxy`** / **`NewOllamaProxyWithHTTPClient`** – constructors; the second is a test seam.
- **`AllowedModelsFromRegistry()`** – derives the allowlist from `KnownGoodModels`.
- **`StartForVM(vmID, vsockPath)`** – binds `<vsockPath>_1025`; starts a goroutine to serve that VM.
- **`StopForVM(vmID)`** / **`Stop()`** – listener teardown.
- **`handleRequest(vmID, req)`** – model-allowlist gate → Ollama HTTP call → streaming decode → audit log.
- **`decodeOllamaChatBody(body, onChunk)`** – NDJSON streaming decoder; accumulates content, thinking/reasoning fields, and tool calls; extracts `<think>` / `<thought>` inline tags.
- **`fetchFallbackThinking(req)`** – secondary Ollama call to elicit reasoning from models that don't emit a native thinking channel.
- **`stripOrphanedThinkingTags(content)`** – cleans up incomplete `<think>` / `<thought>` tags left in content.

## System Role
Critical infrastructure component: every LLM call made by a skill VM passes through this proxy. Enforces model allowlists, caps payload size (`MaxProxyPayloadBytes = 256 KB`), writes audit entries to the tamper-evident kernel log, and keeps Ollama calls strictly on host loopback.

## Notable Dependencies
- `internal/kernel` – tamper-evident audit logging.
- `net/http` – Ollama API calls.
- `go.uber.org/zap` – structured logging.
