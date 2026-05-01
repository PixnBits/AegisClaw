# enforcer.go

## Purpose
Wraps LLM generation calls with JSON schema validation and automatic retry logic. Ensures that responses from Ollama conform to a declared output schema before returning them to callers, decaying temperature on each retry to push the model toward more deterministic output.

## Key Types / Functions
- **`FieldType`** / **`SchemaField`** – typed field descriptor for JSON output schemas.
- **`OutputSchema`** / **`OutputSchema.Validate()`** – declares expected fields and validates a parsed JSON map against them.
- **`ReviewSchema`** – pre-built schema for Court reviewer verdicts (`verdict`, `risk_score`, `evidence`, etc.).
- **`CodeGenSchema`** – pre-built schema for code generation responses (`files`, `reasoning`).
- **`Enforcer`** – wraps a `*Client`; retries up to `MaxRetries` times, subtracting `TemperatureDecay` each attempt.
- **`Enforcer.Generate(ctx, EnforcedRequest)`** – completion-style enforced call.
- **`Enforcer.Chat(ctx, ...)`** – chat-style enforced call.
- **`parseAndValidate(raw, schema)`** – tries direct JSON parsing, then extracts JSON from Markdown code blocks.
- **`extractJSON(s)`** – heuristic extraction from `` ```json `` blocks or raw `{…}` spans.
- **`ParseOutputSchema(raw)`** – parses a compact JSON type-descriptor string into an `OutputSchema`.

## System Role
Core quality-gate between raw Ollama responses and the Court review pipeline. Used by `Verifier` and any component requiring structured LLM output.

## Notable Dependencies
- `encoding/json` – JSON parsing and validation.
- `go.uber.org/zap` – retry logging.
- `Client` (ollama.go) – underlying HTTP transport.
