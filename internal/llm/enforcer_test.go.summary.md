# enforcer_test.go

## Purpose
Comprehensive tests for the `Enforcer` type, covering success paths, retry behaviour, temperature decay, markdown-wrapped JSON extraction, server errors, and schema validation edge cases.

## Key Functions / Test Cases
- **`newTestEnforcer(handler)`** – helper that starts an `httptest.Server` and returns a wired `Enforcer`.
- **`TestOutputSchemaValidate`** – validates required/optional fields and wrong-type detection.
- **`TestFieldTypeString`** – table-driven `FieldType.String()` coverage.
- **`TestExtractJSON`** – verifies JSON extraction from plain JSON, `` ```json `` blocks, plain `` ``` `` blocks, and embedded `{…}` spans.
- **`TestParseOutputSchema`** / `TestParseOutputSchemaOptionalField` – schema string parsing including the `"optional "` / `"?"` prefix convention.
- **`TestEnforcerGenerateSuccess`** – happy path: valid JSON returned on first attempt.
- **`TestEnforcerGenerateRetry`** – ensures the enforcer retries on invalid JSON and succeeds on the third call.
- **`TestEnforcerGenerateExhausted`** – confirms error returned after all retries fail.
- **`TestEnforcerTemperatureDecay`** – captures temperatures sent to the mock server and asserts linear decay per retry.
- **`TestEnforcerGenerateMarkdownWrapped`** – confirms that LLM responses wrapped in `` ```json `` fences are accepted.
- **`TestEnforcerChatSuccess`** / **`TestEnforcerChatRetry`** – parallel coverage for the chat path.
- **`TestBuildOptionsWithTokens`** – `buildOptions` nil/non-nil cases.

## System Role
Regression guard for the structured-output layer relied on by the Court review pipeline.

## Notable Dependencies
- `net/http/httptest` – in-process mock Ollama server.
- `sync/atomic` – concurrent call counting for retry tests.
