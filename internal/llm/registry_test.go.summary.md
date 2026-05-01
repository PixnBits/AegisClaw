# registry_test.go

## Purpose
Unit tests for the `ModelRegistry` type, covering persistence, CRUD operations, tag filtering, validation, and corruption handling.

## Key Test Cases
- **`TestNewModelRegistry`** – empty registry when file does not exist.
- **`TestModelRegistryRegisterAndGet`** – round-trip: register an entry, retrieve it, check SHA256 and tag.
- **`TestModelRegistryPersistence`** – writes a registry in one instance; reloads it in a second; verifies data survives.
- **`TestModelRegistryList`** – two entries appear in the list.
- **`TestModelRegistryByTag`** – tag-filtered query returns the correct subset.
- **`TestModelRegistryRemove`** – entry is absent after removal and count drops to zero.
- **`TestModelRegistryRegisterValidation`** – empty `Name` or empty `SHA256` returns an error.
- **`TestModelRegistryCorruptFile`** – non-JSON file content yields an error on load.
- **`TestModelEntryHasTag`** – membership check for present and absent tags.
- **`TestModelRegistryUpdate`** – re-registering with a new SHA256 overwrites; count stays at 1.

## System Role
Ensures the persistent model metadata store is reliable. All tests use `t.TempDir()` for isolation; no shared state between test cases.

## Notable Dependencies
- `os` – manual file writes for corruption test.
- `path/filepath` – temp-dir path construction.
