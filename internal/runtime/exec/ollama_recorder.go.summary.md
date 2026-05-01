# ollama_recorder.go

## Purpose
A test-only cassette record/replay system for Ollama HTTP interactions, compiled with the `inprocesstest` build tag. It wraps the Ollama HTTP transport with a VCR (Video Cassette Recorder) layer: in record mode it forwards requests to a real Ollama instance and saves the interactions; in replay mode it serves responses from saved cassettes without requiring a live Ollama server. Cassette files are named `<name>-<NNN>.json`.

## Key Types and Functions
- `OllamaRecorder`: struct holding the cassette transport and configuration
- `NewOllamaRecorder(cassetteName string) *OllamaRecorder`: creates a recorder; checks for `RECORD_OLLAMA=true` env var to enable record mode; defaults to replay
- `HTTPClient() *http.Client`: returns an HTTP client backed by the recorder transport
- Cassette format: JSON files with recorded request/response pairs indexed by sequence number
- Record mode: enabled by `RECORD_OLLAMA=true`; refreshes the cassette with live Ollama responses

## Role in the System
Enables deterministic, fast, and network-independent integration tests for the `InProcessTaskExecutor` and the ReAct FSM. By replaying pre-recorded Ollama responses, tests run in CI without needing a GPU or a running Ollama server, while still exercising the full agent reasoning loop.

## Dependencies
- Build tag: `inprocesstest`
- `net/http`: transport wrapping
- `encoding/json`: cassette serialisation
- `os`: environment variable for record/replay mode selection
