# `pipeline_test.go` — Pipeline Tests

## Purpose
Unit tests for `Pipeline` construction validation, `PipelineResult` data model, `computeFileHashes`, and `PipelineState` constants — exercised without a live Firecracker VM or git repository.

## Key Tests

| Test | What It Verifies |
|------|-----------------|
| `TestNewPipelineValidation` | Nil builder runtime, code generator, git manager, kernel, or proposal store each return the expected error. |
| `TestPipelineStateConstants` | Asserts string values of all `PipelineState` constants. |
| `TestComputeFileHashes` | Single file, multiple files, and empty map cases; checks hex length (64 chars) and content-determinism. |
| `TestPipelineResultJSON` | `PipelineResult` serialises and deserialises correctly including nested `AnalysisResult` and `SecurityGateResult`. |
| `TestPipelineSetSBOMDir` | Calling `SetSBOMDir` on a constructed pipeline stores the value without error. |
| `TestPipelineSetWorkspaceSkillContext` | Stores the workspace SKILL.md context correctly. |

## How It Fits Into the Broader System
These tests lock down the `Pipeline` constructor contract and the correctness of its data helpers, preventing regressions in the build pipeline's configuration and result serialisation.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/builder/securitygate`
- Standard library `encoding/json`, `testing`.
