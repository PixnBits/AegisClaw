# sbom_helpers.go — cmd/aegisclaw

## Purpose
Thin wrapper around the `sbom` package for reading SBOM files from disk within the CLI context.

## Key Types / Functions
- `sbomPkg` — type alias for `sbom.SBOM` to avoid import collision with the `skill_cmd.go` package reference.
- `readSBOMFile(path)` — opens and parses an SBOM JSON file; returns a typed `sbom.SBOM` or an error.

## System Fit
Used by `skill_cmd.go` to display SBOM information without re-implementing the parsing logic.

## Notable Dependencies
- `github.com/PixnBits/AegisClaw/internal/sbom`
