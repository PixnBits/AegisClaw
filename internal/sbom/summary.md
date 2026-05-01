# Package: sbom

## Overview
The `sbom` package generates CycloneDX 1.4 JSON Software Bill of Materials (SBOM) documents for AegisClaw skills. An SBOM captures the skill's identity, all software component dependencies with versions and cryptographic hashes, and AegisClaw-specific metadata (proposal ID, generation timestamp, source tag). A deterministic aggregate SHA-256 hash over all sorted file hashes serves as a fingerprint for the skill's entire source corpus.

## Files
- `sbom.go`: SBOM generator, CycloneDX type definitions, `Generate`, `Write`, `Read`, `extractGoModules`, `parseGoMod`
- `sbom_test.go`: Tests for generation, round-trip serialisation, hash determinism, and Go module extraction

## Key Abstractions
- `SBOM`: top-level CycloneDX 1.4 document with metadata, components, and custom properties
- `BuildInfo`: input descriptor containing skill name, version, language, file contents, file hashes, and proposal ID
- `Generate`: the primary entry point; produces a complete, signed SBOM with deterministic aggregate hash
- `extractGoModules`: two-stage dependency extraction — prefer `go.mod`, fall back to import scanning
- Custom properties: `aegisclaw:proposal_id` links the SBOM to the governance approval record

## System Role
SBOMs are generated during the skill build phase after governance approval and before sandbox launch. They are stored alongside the deployed skill and surfaced in the Governance Court dashboard and audit logs. This enables operators to audit the full dependency tree of any running skill, satisfying supply-chain security requirements (e.g., SLSA).

## Dependencies
- `encoding/json`: CycloneDX JSON serialisation with pretty-printing
- `crypto/sha256`: aggregate file hash computation
- `github.com/google/uuid`: `urn:uuid:` serial number generation
- `sort`, `bufio`, `strings`: deterministic ordering and `go.mod` parsing
