# manager_test.go

## Purpose
Tests for the `Manager` type, using an in-process mock Ollama HTTP server to isolate all Ollama API calls.

## Key Helpers / Test Cases
- **`testLogger()`** – creates a development-mode zap logger for tests.
- **`testRegistry(t)`** – creates a `ModelRegistry` backed by a temp-dir JSON file.
- **`ollamaMockServer(t, models)`** – `httptest.Server` that handles `/api/tags`, `/api/show`, and `/api/pull` with canned responses.
- **`TestManagerListStatus`** – verifies that registered + available + matching-hash models are marked `Verified`.
- **`TestManagerListStatusUnregistered`** – confirms models available in Ollama but absent from the registry are surfaced with `Registered=false`.
- **`TestManagerVerify`** – happy path: digest matches registered SHA256, `Verified=true`.
- **`TestManagerVerifyMismatch`** – wrong digest yields `Verified=false` without error.
- **`TestManagerVerifyNotAvailable`** – server returns 404; expects error and `Available=false`.
- **`TestManagerUpdate`** – pulls a model, checks registry is persisted with the correct digest.
- **`TestManagerUpdatePullError`** – server 500; expects error returned.
- **`TestManagerSyncKnownGood`** – seeds all `KnownGoodModels` and asserts existing entries are not overwritten.
- **`TestNormalizeModelName`** / **`TestTagsForModel`** – unit tests for helper functions.

## System Role
Ensures the model lifecycle orchestration layer correctly handles availability, verification, and persistence under all expected conditions.

## Notable Dependencies
- `net/http/httptest` – in-process mock server.
- `go.uber.org/zap` – logger construction.
