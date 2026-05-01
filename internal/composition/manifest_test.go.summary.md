# `manifest_test.go` — Tests for the Composition Manifest Store

## Purpose
Provides full coverage for `manifest.go`. Tests span the complete lifecycle of the `Store`: creation, publishing, retrieval, health updates, rollback, history ordering, disk persistence, and manifest validation.

## Key Test Cases

| Test | What It Verifies |
|---|---|
| `TestNewStore` | Empty store starts at version 0 with nil current manifest |
| `TestPublishAndCurrent` | Sequential publishes increment the version counter and update `Current()` |
| `TestPublishValidation` | Empty components and missing component versions are rejected |
| `TestGet` | Specific version retrieval; non-existent version returns error |
| `TestRollback` | Rollback to v1 creates a new version (v3) carrying v1's components |
| `TestRollbackToPrevious` | Fails on v1-only store; succeeds from v2 |
| `TestUpdateHealth` | Marks component unhealthy; unknown component returns error |
| `TestNeedsRollback` | Empty store → false; all healthy → false; any unhealthy → true |
| `TestHealthSummary` | Counts across all four health states (unknown treated as unhealthy) |
| `TestHistory` | Returns manifests sorted by version |
| `TestPersistence` | `v1.json` written to disk; new `Store` instance reloads same version |
| `TestManifestValidate` | Table-driven: zero version, empty components, name mismatch, empty version |
| `TestComponentHubType` | `ComponentHub` is distinct; hub entry round-trips through `Publish` and reload |

## Role in the System
Guards against regressions in the rollback mechanism, which is a critical safety path invoked automatically when in-VM health checks signal degradation.

## Notable Dependencies
- Package under test: `composition`
- Standard library only (`os`, `path/filepath`, `testing`)
