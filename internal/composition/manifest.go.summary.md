# `manifest.go` — Versioned Composition Manifest Store

## Purpose
Implements the versioned composition manifest system that tracks which component versions are currently deployed and supports automatic rollback when components become unhealthy. Resolves deviation D10 from the PRD alignment plan (§13.2).

## Key Types & Functions

| Symbol | Description |
|---|---|
| `ComponentType` | Enum: `skill`, `reviewer`, `builder`, `main-agent`, `coordinator`, `hub` |
| `HealthStatus` | Enum: `healthy`, `degraded`, `unhealthy`, `unknown` |
| `Component` | Single managed component: name, type, version, sandbox ID, artifact ref, rootfs hash, health |
| `Manifest` | Versioned snapshot of all active components; includes SHA-256 hash and audit metadata |
| `Store` | Thread-safe, disk-backed manager of versioned manifests |
| `Store.Publish()` | Creates a new manifest version from a component map |
| `Store.Rollback()` | Promotes a prior version as a new manifest entry |
| `Store.RollbackToPrevious()` | Convenience rollback to version N-1 |
| `Store.UpdateHealth()` | Mutates a component's health status in the current manifest |
| `Store.NeedsRollback()` | Returns `true` if any component is unhealthy/unknown |
| `Manifest.ComputeHash()` | Deterministic SHA-256 over component JSON |
| `Manifest.HealthSummary()` | Counts healthy/degraded/unhealthy components |

## Role in the System
This package is the single source of truth for what *should* be running. The daemon reads `Store.Current()` at startup to re-launch components, calls `UpdateHealth()` when in-VM health checks report degradation, and triggers `RollbackToPrevious()` when `NeedsRollback()` is true. The `ComponentHub` type ensures AegisHub (the IPC router microVM) is always the first entry in any manifest.

## Notable Dependencies
- Standard library: `crypto/sha256`, `encoding/json`, `os`, `sync`, `time`
- No external packages
