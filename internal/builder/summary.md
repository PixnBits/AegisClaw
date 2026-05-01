# `internal/builder/` — Package Summary

## Overview
Package `builder` implements AegisClaw's automated skill-build subsystem. Starting from an approved `proposal.Proposal` and a `SkillSpec`, it coordinates Firecracker MicroVM sandboxes, an LLM-based code generator, static analysis, mandatory security gates, git operations, and signed artifact packaging into a single coherent pipeline.

## Architecture

```
Pipeline.Execute
  ├─ BuilderRuntime.LaunchBuilder   (Firecracker VM, semaphore-limited)
  ├─ CodeGenerator.Generate         (vsock → Ollama, PromptTemplate)
  ├─ git.Manager.CommitFiles        (proposal branch)
  ├─ Analyzer.Analyze               (go test + golangci-lint + gosec + go build)
  ├─ securitygate.Pipeline.Evaluate (SAST + SCA + secrets + policy)
  ├─ sbom.Generate / Write          (optional SBOM JSON)
  └─ ArtifactStore.PackageArtifact  (Ed25519-signed binary + manifest)

IterationEngine.RunFixLoop          (rounds 2–4, driven by Court feedback)
  └─ CodeGenerator → Commit → Analyzer (loop until pass or exhausted)
```

## File Table

| File | Role |
|------|------|
| `analysis.go` | `AnalysisFinding`, `AnalysisResult`, `Analyzer`, output parsers |
| `analysis_test.go` | Data model and parser unit tests |
| `artifact.go` | `ArtifactManifest`, `SandboxManifest`, `ArtifactStore` |
| `artifact_test.go` | Signing, verification, and tamper-detection integration tests |
| `builder.go` | `BuilderSpec`, `BuilderConfig`, `BuilderRuntime` (VM lifecycle) |
| `builder_test.go` | Spec/config validation unit tests |
| `codegen.go` | `SkillSpec`, `ToolSpec`, `CodeGenerator`, `PromptTemplate`, `DefaultTemplates` |
| `codegen_test.go` | Spec validation, template formatting, generator construction tests |
| `iteration.go` | `IterationEngine`, `FixRequest`, `FixRound`, `ExtractFeedback` |
| `iteration_test.go` | Fix-loop logic and feedback extraction tests |
| `pipeline.go` | `Pipeline`, `PipelineResult` (end-to-end orchestrator) |
| `pipeline_test.go` | Pipeline construction, file hashing, state constant tests |

## Subpackage

| Subpackage | Role |
|------------|------|
| `securitygate/` | SAST, SCA, secrets scanning, and policy-as-code gates (PRD §11.2 / D8) |

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/kernel` — control plane, Ed25519 signing, audit log
- `github.com/PixnBits/AegisClaw/internal/sandbox` — `FirecrackerRuntime`
- `github.com/PixnBits/AegisClaw/internal/git` — branch/commit/diff management
- `github.com/PixnBits/AegisClaw/internal/proposal` — `Proposal`, `Review` types
- `github.com/PixnBits/AegisClaw/internal/sbom` — SBOM generation
- `go.uber.org/zap`, `github.com/google/uuid`
