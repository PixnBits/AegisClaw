# sbom.go

## Purpose
Generates CycloneDX 1.4 JSON Software Bill of Materials (SBOM) documents for deployed skills. Each SBOM captures the skill's identity, all component dependencies with versions and hashes, and AegisClaw-specific metadata such as the associated proposal ID. The aggregate hash is computed as a sorted SHA-256 over all file hashes, providing a deterministic fingerprint of the skill's full source. SBOMs are written to `sbom.json` in the skill's build directory and can be read back for verification.

## Key Types and Functions
- `SBOM`, `Metadata`, `Component`, `Hash`, `LicenseRef`, `Property`: CycloneDX 1.4 JSON structures
- `BuildInfo`: input to generation — SkillName, SkillDescription, Version, Language, Files (map[string]string content), FileHashes, ProposalID
- `Generate(info BuildInfo) *SBOM`: produces a complete SBOM with sorted file-hash aggregation; serial number as `urn:uuid:<uuid>`; custom properties `aegisclaw:proposal_id`, `aegisclaw:generated_at`, `aegisclaw:source`
- `extractGoModules(files map[string]string) []Component`: prefers `go.mod` for dependency versions; falls back to import scanning for `.go` files
- `parseGoMod(content string) []Component`: handles both block (`require ( ... )`) and inline (`require pkg v`) forms
- `Write(dir string, sbom *SBOM) (string, error)`: serialises to `sbom.json` with indentation
- `Read(path string) (*SBOM, error)`: deserialises from file

## Role in the System
Generated during the skill build phase after governance approval. The SBOM is attached to the deployment record and surfaced in the governance proposal details, enabling supply-chain auditing of every deployed skill.

## Dependencies
- `encoding/json`: CycloneDX JSON serialisation
- `crypto/sha256`: aggregate file hash computation
- `github.com/google/uuid`: serial number generation
- `sort`, `strings`, `bufio`: deterministic hash ordering and go.mod parsing
