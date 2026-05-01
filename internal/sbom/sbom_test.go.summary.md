# sbom_test.go

## Purpose
Tests for the SBOM generator covering the full round-trip lifecycle plus edge cases in dependency extraction. Verifies that `Generate` produces a well-formed CycloneDX 1.4 document, that `Write`/`Read` preserves all fields, that the aggregate hash changes when file contents change, and that Go module parsing handles both inline and block `require` forms correctly.

## Key Types and Functions
- `TestGenerate_BasicFields`: verifies serial number format, component metadata, and presence of custom aegisclaw properties in generated SBOM
- `TestGenerate_AggregateHash`: creates two `BuildInfo` values differing in one file and verifies the aggregate SHA-256 differs
- `TestWriteRead_RoundTrip`: writes an SBOM to a temp directory and reads it back; verifies all fields are preserved
- `TestExtractGoModules_GoMod`: provides a `go.mod` with block requires and verifies all dependencies appear as components with correct versions
- `TestExtractGoModules_Fallback`: provides `.go` source files without a `go.mod` and verifies import-scan produces components
- `TestParseGoMod_BlockForm`: exercises the block `require ( ... )` parser
- `TestParseGoMod_InlineForm`: exercises the inline `require pkg v` parser
- `TestSortedHashes`: verifies that aggregate hash is stable regardless of map iteration order

## Role in the System
Ensures SBOM generation is deterministic and correct. Since SBOMs are used for supply-chain auditing of deployed skills, correctness is a compliance requirement.

## Dependencies
- `testing`, `t.TempDir()`
- `internal/sbom`: package under test
