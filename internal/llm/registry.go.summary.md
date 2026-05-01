# registry.go

## Purpose
Provides a thread-safe, JSON-file-backed store for known-good LLM model entries. Each entry carries the model name, a SHA256 digest for integrity verification, persona-suitability tags, and an optional size hint.

## Key Types / Functions
- **`ModelEntry`** – serialisable struct: `Name`, `SHA256`, `Tags []string`, `SizeHint`.
- **`ModelEntry.HasTag(tag)`** – O(n) tag membership check.
- **`ModelRegistry`** – in-memory map guarded by `sync.RWMutex`; persists to a JSON file on every mutation.
- **`NewModelRegistry(path)`** – loads from disk if the file exists; creates an empty registry if not; fails on corrupt JSON.
- **`Registry.Get(name)`** – returns a single entry by name.
- **`Registry.List()`** – returns all entries as a slice.
- **`Registry.ByTag(tag)`** – filters entries by persona tag.
- **`Registry.Register(entry)`** – upserts an entry; requires non-empty `Name` and `SHA256`; persists atomically.
- **`Registry.registerSeed(entry)`** – like `Register` but allows empty SHA256 (used by `Manager.SyncKnownGood`).
- **`Registry.Remove(name)`** – deletes and persists.
- **`Registry.Count()`** – returns entry count.

## System Role
Single source of truth for approved model metadata. Consumed by `Manager` for verification, by `Router` for persona→model resolution, and by `OllamaProxy` to build the runtime allowlist.

## Notable Dependencies
- `encoding/json` – serialisation.
- `os` – file read/write.
- `sync` – reader/writer mutex.
