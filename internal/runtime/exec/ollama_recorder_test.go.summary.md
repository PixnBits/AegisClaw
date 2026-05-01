# ollama_recorder_test.go

## Purpose
Tests for `OllamaRecorder` — the VCR (cassette record/replay) mechanism used to run agent integration tests without a live Ollama instance. Gated by the `inprocesstest` build tag so it only runs in designated test environments.

## Key Types / Functions
- `writeFixture` — helper that writes a single cassette JSON file (`request` + `response` pair) to a temp directory, mimicking the on-disk format produced by `OllamaRecorder`.
- `formatIdx` / `paddedInt` / `pow10` — zero-padded index helpers that mirror `OllamaRecorder`'s file naming convention.
- `TestOllamaRecorder_Replay` — verifies sequential cassette playback returns pre-recorded turns in order.
- `TestOllamaRecorder_ReplayMissingCassette` — asserts a descriptive error with a `RECORD_OLLAMA=true` hint when the cassette file is absent.
- `TestOllamaRecorder_Record` — sets `RECORD_OLLAMA=true` and confirms that two cassette files are written after two calls to the real agent function.
- `TestOllamaRecorder_IntegratesWithReActRunner` — end-to-end test: wires an `OllamaRecorder` into an `InProcessExecutor`, drives a `ReActRunner` through one tool-call + final-answer, and validates the full transition sequence and result.

## System Fit
These tests close the loop between cassette replay and the ReAct FSM, proving that pre-recorded fixtures can drive a full agent run deterministically.

## Notable Dependencies
- `inprocesstest` build tag (also guards `ollama_recorder.go` and `inprocess_executor.go`)
- `os`, `path/filepath` — cassette file I/O
- `github.com/PixnBits/AegisClaw/internal/runtime/exec` — package under test
