# manager.go

## Purpose
Provides high-level model lifecycle management: listing combined registration/availability status, verifying digest integrity, pulling updated models from Ollama, and seeding the registry with known-good defaults.

## Key Types / Functions
- **`KnownGoodModels`** – package-level slice of `ModelEntry` values representing officially approved models (mistral-nemo, llama3.2:3b, gemma4 variants, qwen2.5-coder, nemotron-mini), each annotated with persona-suitability tags.
- **`ManagerConfig`** – holds the read-only model directory path.
- **`Manager`** – orchestrates a `*Client` and a `*ModelRegistry`.
- **`Manager.ListStatus(ctx)`** – merges registry entries with live Ollama output, returning `[]ModelStatus` with `Registered`, `Available`, and `Verified` flags.
- **`Manager.Verify(ctx, name)`** – checks a single model's SHA256 digest against its registered hash.
- **`Manager.Update(ctx, name)`** – pulls a model via `Client.Pull`, extracts the digest, and registers it.
- **`Manager.SyncKnownGood()`** – idempotently seeds `KnownGoodModels` into the registry without overwriting existing entries.
- **`normalizeModelName(name)`** – strips `:latest` tag for consistent name comparison.
- **`tagsForModel(name)`** – looks up persona tags from `KnownGoodModels`.

## System Role
Top-level management layer used by CLI commands and startup routines to ensure models are present, up-to-date, and cryptographically verified before use.

## Notable Dependencies
- `Client` (ollama.go) – Ollama HTTP calls.
- `ModelRegistry` (registry.go) – persistent model metadata store.
- `go.uber.org/zap` – operation logging.
