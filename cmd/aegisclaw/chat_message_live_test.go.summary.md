# chat_message_live_test.go — cmd/aegisclaw

## Purpose
Live integration tests for the `chat.message` handler that require root, KVM, and a running Firecracker environment. Tests are automatically skipped when `os.Getuid() != 0` or `/dev/kvm` is unavailable.

## Key Functions / Helpers
- `runChatMessageLiveScenario(t, cassetteName, sessionID, fn)` — boots a real VM, replays an Ollama cassette, and invokes the scenario function with a chat-send helper.
- `uuidInText` regexp — used to normalise UUIDs in assertion strings.
- Individual scenarios cover: simple Q&A, proposal creation, tool-call sequences, and error recovery.

## System Fit
The most faithful test of the full agent stack. Skipped in CI without KVM; run explicitly on bare-metal dev machines or in the live-test CI job.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/llm` — OllamaRecorder
- `github.com/PixnBits/AegisClaw/internal/testutil`
- Requires `/dev/kvm`, root privileges, and rootfs images.
