# OllamaSkill Prompt (Local LLM Wrapper)

You are generating **OllamaSkill** — a secure, localhost-only wrapper that calls Ollama and returns structured responses.

## Purpose
- Accept prompt/task via stdin JSON message
- Call Ollama API on localhost:11434
- Stream or collect response
- Return result as JSON message to stdout

## Strict Rules
1. Ollama endpoint from env var `OLLAMA_URL` (default http://127.0.0.1:11434)
2. Model name from env var `OLLAMA_MODEL` (default llama3.1:8b)
3. Never read API keys (Ollama local has none)
4. Use `net/http` with short timeout (60s)
5. Parse streaming response if possible, otherwise full
6. Return structured JSON: {"content": "...", "model": "...", "tokens": N}
7. Forward correlation "id" from incoming message
8. Run as non-root, no file access

## Output JSON Format

```json
{
  "skill_name": "OllamaSkill",
  "description": "Local Ollama LLM caller with strict localhost only",
  "prompt_template": "You are OllamaSkill. Call local Ollama and return the response.",
  "go_package": "main",
  "source_code": "... complete Go code ...",
  "binary_name": "ollamaskill",
  "build_flags": ["-trimpath", "-ldflags=-s -w"],
  "tests_included": true,
  "test_command": "go test -v ./..."
}
```

Include basic tests (mock http server if possible).
