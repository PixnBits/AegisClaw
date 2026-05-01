# Package `internal/composition` — Versioned Composition Manifest Store

## Purpose
Implements a versioned, disk-backed manifest system that tracks the exact component versions currently deployed in AegisClaw microVMs. Provides automatic rollback when in-VM health checks signal degradation. Resolves PRD deviation D10 (§13.2 versioned composition model).

## Files

| File | Description |
|---|---|
| `manifest.go` | `Store`, `Manifest`, `Component`, `HealthStatus`, `ComponentType` — core implementation |
| `manifest_test.go` | Full lifecycle tests: publish, rollback, health updates, persistence, validation |

## Key Abstractions

- **`Manifest`** — immutable snapshot of deployed components at a point in time; identified by a monotonically increasing integer version and a deterministic SHA-256 hash
- **`Store`** — thread-safe manager: publishes new versions, loads the latest on startup, supports targeted or previous-version rollback
- **`ComponentType`** — classifies components (`skill`, `reviewer`, `builder`, `hub`, etc.); the special `hub` type ensures AegisHub is always tracked distinctly
- **`HealthStatus`** — `healthy` / `degraded` / `unhealthy` / `unknown`; unknown is treated as unhealthy for rollback decisions

## How It Fits Into the Broader System

The composition store is read by the daemon at startup to re-launch the correct component versions. Component health reporters (in-VM controllers) call `UpdateHealth()` on degradation, and the daemon's health-watch goroutine calls `NeedsRollback()` → `RollbackToPrevious()` when any component is unhealthy. Each manifest version is written as `v<N>.json` in the configured composition directory.

## Notable Dependencies
- Standard library only: `crypto/sha256`, `encoding/json`, `os`, `path/filepath`, `sync`, `time`
