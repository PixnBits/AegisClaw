# `artifact_test.go` — Artifact Store Tests

## Purpose
Integration-style tests for `ArtifactStore.PackageArtifact` and `VerifyArtifact`. Tests use a real kernel instance backed by a temp directory to exercise the full signing and verification path.

## Key Tests

| Test | What It Verifies |
|------|-----------------|
| `TestNewArtifactStoreValidation` | Empty base dir or nil kernel returns an error. |
| `TestPackageAndVerifyArtifact` | Packages a fake binary; asserts manifest fields (`SkillID`, `BinaryHash`, `Signature`, `SandboxManifest`), checks files exist on disk (`skill`, `manifest.json`, `manifest.sig`, `SHA256SUMS`), then calls `VerifyArtifact` and expects success. |
| Tamper tests (binary/manifest) | Flip one byte in the written binary or manifest and assert `VerifyArtifact` returns an error. |
| Path traversal tests | Skill IDs containing `..` or `/` are rejected by `PackageArtifact` and `VerifyArtifact`. |
| `TestListArtifacts` | Creates two skills; `ListArtifacts` returns both IDs. |
| `TestGetManifest` | Packages an artifact and reads it back via `GetManifest`. |

## How It Fits Into the Broader System
Ensures the supply-chain integrity guarantees of the artifact store hold under adversarial conditions (tampered files, malformed skill IDs).

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/kernel`
- `go.uber.org/zap`, standard library `os`, `encoding/hex`, `testing`.
