# ollama_test.go

## Purpose
Unit tests for all `Client` methods in `ollama.go`, using `httptest.Server` to replay canned Ollama API responses without touching a real Ollama process.

## Key Test Cases
- **`TestNewClient`** – default endpoint (`OllamaEndpoint`) when `ClientConfig` is empty.
- **`TestNewClientCustomEndpoint`** / **`TestNewClientCustomTimeout`** – config overrides.
- **`TestGenerate`** – verifies correct path (`/api/generate`), POST method, JSON content type, `stream=false`, and response decoding.
- **`TestChat`** – verifies chat path, `stream=false`, and message count.
- **`TestList`** – verifies GET `/api/tags` and list response decoding.
- **`TestShow`** – verifies POST `/api/show` and `ModelDetails` decoding.
- **`TestPull`** – verifies POST `/api/pull` and digest in response.
- **`TestHealthy`** / **`TestHealthyFail`** – liveness check against responsive and unreachable servers.
- **`TestGenerateServerError`** / **`TestChatServerError`** – non-200 responses propagate as errors.
- **`TestGenerateWithOptions`** – `Temperature` and `Format` fields flow through to the server.

## System Role
Fast, self-contained regression suite for the Ollama HTTP transport layer. Any API contract breakage shows up here before touching higher layers.

## Notable Dependencies
- `net/http/httptest` – in-process mock Ollama server.
- `encoding/json` – request inspection and response encoding.
