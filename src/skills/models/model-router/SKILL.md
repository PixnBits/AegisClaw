# LLMSelectorSkill Prompt

You are generating **LLMSelectorSkill** — an intelligent router that decides which LLM wrapper skill should handle a given prompt/task.

## Purpose
- Receive incoming task messages via stdin (JSON envelope)
- Analyze prompt content and metadata
- Choose the best available LLM skill (OllamaSkill, GrokSkill, etc.)
- Forward the task to that skill via stdout (using message hub format)
- Return the response back through the hub

## Selection Heuristics (hard-code these rules, prioritize coder models for code tasks)
- Contains "code", "golang", "compile", "generate skill", "fix bug", "refactor", "test" → prefer code-specialized model: qwen2.5-coder:32b (or :14b/:7b fallback) → highest priority
- Contains "reason", "analyze", "explain", "research" → prefer general-reasoning model (e.g. llama3.3:70b or deepseek-r1)
- Short/quick/simple → fastest local model (qwen2.5-coder:7b or llama3.2:3b)
- Mentions "x.ai", "grok", "real-time" → GrokSkill if available
- Default → qwen2.5-coder:32b (strongest open coder available)

## Strict Rules
1. Use only stdlib + basic json/time packages.
2. Discover available LLM skills by listening for their registration messages.
3. Accept config via env var: LLM_SELECTOR_PREFERENCES (JSON string, optional)
4. Forward full task payload unmodified, only change "to" field.
5. Include reasoning in a "metadata" field in the forwarded message.
6. Timeout forwarding after 90 seconds.
7. Discover available models via env var OLLAMA_MODELS (comma-separated list) or registration messages from LLM wrappers.
8. If qwen2.5-coder variants are present, bias strongly toward them for any code-related payload.

## Output JSON Format

```json
{
  "skill_name": "LLMSelectorSkill",
  "description": "Routes prompts to the most appropriate LLM wrapper skill",
  "prompt_template": "You are LLMSelectorSkill. Choose best LLM based on heuristics and forward the task.",
  "go_package": "main",
  "source_code": "... complete Go code ...",
  "binary_name": "llmselector",
  "build_flags": ["-trimpath", "-ldflags=-s -w"],
  "tests_included": true,
  "test_command": "go test -v ./..."
}
```

Generate complete single-file implementation + tests.
