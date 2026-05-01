# Package: testutil

## Overview
The `testutil` package provides shared test helpers for the AegisClaw test suite. Currently it contains a VCR-style Ollama HTTP recorder that enables integration tests for the ReAct agent loop to run without a live Ollama server. Cassette files are stored in `testdata/cassettes/` and committed to the repository, ensuring reproducible CI runs.

## Files
- `ollama_recorder.go`: `NewOllamaRecorderClient`, `matchNormalizedOllamaRequest`, `OllamaCassetteExists`, normalisation helpers, and test constants

## Key Abstractions
- `NewOllamaRecorderClient`: the primary entry point; returns an `*http.Client` whose transport is backed by go-vcr; toggled between record and replay via `RECORD_OLLAMA` env var
- `matchNormalizedOllamaRequest`: custom cassette matcher that normalises non-deterministic fields (UUIDs, timestamps) to enable stable replay across different run times
- `cassetteBasePath`: source-relative path resolution so cassettes are found regardless of the test working directory
- Test constants: `TestOllamaSeed = 42`, `TestOllamaTemperature = 0` for deterministic inference

## System Role
Consumed by tests in `internal/runtime/exec` (via the `inprocesstest` build tag) to replay pre-recorded Ollama conversations when testing the `InProcessTaskExecutor` and `ReActRunner`. It decouples the test suite from external service availability while still exercising real agent reasoning paths.

## Dependencies
- `gopkg.in/dnaeon/go-vcr.v2`: HTTP cassette recording/replay
- `net/http`: underlying transport
- `testing`: test lifecycle integration
- `runtime`: `Caller(0)` for source-relative cassette path resolution
