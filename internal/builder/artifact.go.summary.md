# `artifact.go` — Signed Build Artifact Store

## Purpose
Manages the packaging, signing, and verification of compiled skill binaries on disk. After a successful build, `ArtifactStore` signs the binary and a JSON manifest with the kernel's Ed25519 key, writes them under `<baseDir>/<skillID>/`, and produces a `SHA256SUMS` checksum file.

## Key Types / Functions

| Symbol | Description |
|--------|-------------|
| `ArtifactType` | Enum: `binary`, `manifest`, `source`. |
| `ArtifactManifest` | Full build provenance: `SkillID`, `ProposalID`, `Version`, `CommitHash`, `BinaryPath/Hash/Size`, `FileHashes`, `Signature`, `KernelPubKey`, embedded `SandboxManifest`. |
| `SandboxManifest` | Deployment metadata: vCPUs, memory, disk, network policy, secrets refs, read-only root, entry command. |
| `ArtifactStore` | Persists artifacts; uses `kern.Sign` for Ed25519 signatures and `kern.SignAndLog` for audit entries. |
| `ArtifactStore.PackageArtifact` | Validates skill ID (path-traversal guard), computes SHA-256, signs binary and manifest, writes `skill`, `manifest.json`, `manifest.sig`, `SHA256SUMS`. |
| `ArtifactStore.VerifyArtifact` | Reads and validates the manifest, checks the manifest signature, re-hashes the binary, and verifies the binary signature. |
| `ArtifactStore.ListArtifacts` | Returns all skill IDs that have a `manifest.json`. |
| `ArtifactStore.GetManifest` | Returns the parsed manifest for a skill ID. |

## How It Fits Into the Broader System
Called by the pipeline after a successful build to produce deployment-ready, verifiable skill packages. The `SandboxManifest` inside is consumed by the sandbox runtime at deploy time.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/kernel` — signing and audit.
- `go.uber.org/zap`.
