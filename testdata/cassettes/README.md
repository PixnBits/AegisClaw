# Ollama Cassettes

This directory stores `go-vcr` recordings for Ollama-backed tests.

- Replay is the default test mode.
- Set `RECORD_OLLAMA=true` to refresh a cassette with live Ollama responses.
- The first cassette expected by the live tutorial test is `first-skill-tutorial-live.yaml`.

Example:

```bash
RECORD_OLLAMA=true go test ./cmd/aegisclaw -run TestFirstSkillTutorialLive -v
```