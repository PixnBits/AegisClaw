# Generic LLM Wrapper Template Prompt

Use this as a template when asked to create a wrapper for a remote LLM (Grok, Claude, GPT, etc.).

Replace:
- LLM_NAME      → GrokSkill, ClaudeSkill, etc.
- BINARY_NAME   → grokskill
- ENV_KEY_NAME  → GROK_API_KEY
- API_ENDPOINT  → https://api.x.ai/v1/chat/completions (or equivalent)
- MODEL_VAR     → GROK_MODEL (default grok-beta)

Keep all security rules:
- API key **only** from environment variable
- Never log or print key
- Use TLS, short timeouts
- Structured output parsing when possible
- JSON message envelope in/out

Output JSON format remains the same, just change skill_name, description, binary_name, prompt_template accordingly.
