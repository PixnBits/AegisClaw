# LLM Caller Skill

This skill provides a thin client interface for making calls to Large Language Models (LLMs). It supports multiple providers including local Ollama instances and remote APIs (OpenAI, Claude, Grok, etc.).

## Capabilities

- Makes HTTP requests to LLM APIs
- Handles authentication via environment variables
- Supports structured prompts and responses
- Routes requests through the message-hub for secure communication

## Environment Variables

- `OPENAI_API_KEY`: API key for OpenAI services
- `ANTHROPIC_API_KEY`: API key for Anthropic Claude
- `OLLAMA_BASE_URL`: Base URL for local Ollama instance (default: http://localhost:11434)

## Message Format

### Incoming Messages
```json
{
  "from": "sender",
  "to": "llm-caller",
  "content": {
    "action": "call",
    "provider": "openai",
    "model": "gpt-3.5-turbo",
    "prompt": "Your prompt here",
    "max_tokens": 100
  }
}
```

### Outgoing Messages
```json
{
  "from": "llm-caller",
  "to": "sender",
  "content": {
    "response": "Generated response text",
    "usage": {
      "prompt_tokens": 10,
      "completion_tokens": 20,
      "total_tokens": 30
    }
  }
}
```

## Supported Providers

- `openai`: OpenAI GPT models
- `anthropic`: Anthropic Claude models
- `ollama`: Local Ollama models
- `grok`: xAI Grok (future implementation)