# ollama_recorder.go

## Purpose
Provides a VCR-style HTTP client for Ollama that can record live HTTP interactions and replay them in subsequent test runs without requiring a live Ollama server. When `RECORD_OLLAMA=true` is set, the client forwards requests to a real Ollama instance and saves cassette files. In the default replay mode, it serves responses from pre-recorded YAML cassette files under `testdata/cassettes/`. Request matching normalises UUIDs and datetime strings to avoid spurious mismatches.

## Key Types and Functions
- `NewOllamaRecorderClient(t *testing.T, cassetteName string) *http.Client`: creates a VCR-backed HTTP client; sets record or replay mode based on `RECORD_OLLAMA` env var
- `matchNormalizedOllamaRequest(r1, r2 cassette.Request) bool`: custom matcher that replaces UUIDs with `<id>` and datetime strings with `<datetime>` before comparing
- `OllamaCassetteExists(name string) bool`: checks whether a cassette file exists at `testdata/cassettes/<name>.yaml`
- `cassetteBasePath`: uses `runtime.Caller(0)` to locate the repository root regardless of the working directory
- `Float64(v float64) *float64`: pointer helper for `AgentTurnRequest.Temperature` and `Seed` fields
- Constants: `TestOllamaSeed = 42`, `TestOllamaTemperature = 0`

## Role in the System
Enables deterministic, fast, and CI-friendly integration tests for the `InProcessTaskExecutor` and the ReAct FSM. Test cassettes are committed to the repository so all CI runs replay the same Ollama responses without network access.

## Dependencies
- `gopkg.in/dnaeon/go-vcr.v2`: VCR cassette transport
- `net/http`, `testing`, `runtime`: HTTP client, test lifecycle, source-relative path resolution
