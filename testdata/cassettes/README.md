# Ollama Cassettes

This directory stores [`go-vcr`](https://github.com/dnaeon/go-vcr) HTTP
recordings of Ollama exchanges for live integration tests.

## How it works

- **Replay is the default.**  Normal `go test ./...` runs replay the recorded
  YAML cassettes without touching a live Ollama daemon.  Tests skip
  automatically when the cassette is missing.
- **Set `RECORD_OLLAMA=true`** to hit a live Ollama instance and write a fresh
  cassette.  The recorder normalises UUIDs, short hex IDs, and volatile
  datetime strings so cassettes stay deterministic across re-recordings.

## Cassette inventory

| File | Test | Scenario |
|------|------|----------|
| `chat-message-time-live.yaml` | `TestChatMessageLiveScenarioTimeQuestion` | "What time is it in Phoenix?" |
| `chat-message-hello-world-live.yaml` | `TestChatMessageLiveScenarioHelloWorldSkill` | Full proposal create → submit → court review → activate → invoke |
| `chat-message-solar-live.yaml` | `TestChatMessageLiveScenarioSolarSizing` | Solar panel sizing calculation |
| `first-skill-tutorial-live.yaml` | `TestFirstSkillTutorialLive` (livetest), `TestFirstSkillTutorialInProcess` (inprocesstest) | End-to-end first-skill tutorial |

## Prerequisites for recording

All four of the following must be present:

1. **Run as root** — Firecracker's jailer requires `CAP_SYS_ADMIN`
2. **`/dev/kvm`** — hardware virtualisation for Firecracker microVMs
3. **Alpine rootfs** — `/var/lib/aegisclaw/rootfs-templates/alpine.ext4`
4. **Ollama daemon** — reachable at `127.0.0.1:11434` with the required models:
   ```bash
   ollama pull mistral-nemo:latest   # main agent model
   ollama pull llama3.2:3b           # court reviewer model
   ```

## Regenerating cassettes

### All cassettes at once

```bash
make record-cassettes
```

This runs each scenario in series.  The total runtime is up to ~90 minutes for
the full hello-world scenario.  Prefer running individual scenarios when
iterating:

### Individual scenarios

```bash
# Single time-question turn (fastest, ~5 min)
make record-cassette-time

# Hello-world full lifecycle (create → submit → review → activate → invoke, ~30 min)
make record-cassette-hello-world

# Solar sizing calculation (~5 min)
make record-cassette-solar

# First-skill tutorial end-to-end (~60 min)
make record-cassette-tutorial
```

Or invoke `go test` directly:

```bash
sudo RECORD_OLLAMA=true "$(command -v go)" test ./cmd/aegisclaw -run '^TestChatMessageLiveScenarioTimeQuestion$' -v -count=1
```

## When to regenerate

Regenerate cassettes when:

- Agent system prompt or ReAct loop logic changes (different tool-call
  sequences will no longer match the recorded bodies)
- Ollama or a model version is bumped
- A new tool or API handler changes the response format the agent sees
- The cassette matcher reports mismatches during CI (look for
  `"no interaction found"` errors in the test log)

After recording, commit the updated YAML files alongside the code change.

## Determinism settings

The recorder helper (`internal/testutil/ollama_recorder`) injects:

- **temperature = 0** — removes sampling randomness
- **seed = 42** — makes token selection fully deterministic for models that
  support the `seed` parameter

UUID-like strings, short hex IDs, and volatile datetime strings in request
bodies are normalised before matching so cassettes survive across runs that
generate different IDs or timestamps.
