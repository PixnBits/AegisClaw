package main

import "github.com/PixnBits/AegisClaw/internal/sbom"

// sbomPkg is an alias for sbom.SBOM so skill_cmd.go can reference it
// without importing the package directly (it's already imported here).
type sbomPkg = sbom.SBOM

// readSBOMFile is a thin wrapper so skill_cmd.go stays import-free.
func readSBOMFile(path string) (*sbom.SBOM, error) {
	return sbom.Read(path)
}
