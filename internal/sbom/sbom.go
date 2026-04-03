// Package sbom implements a minimal Software Bill of Materials (SBOM) generator
// following a subset of the CycloneDX 1.4 JSON specification.
//
// An SBOM is emitted automatically when the builder pipeline completes a skill
// and is stored alongside the build artifact.  It captures the skill component
// itself plus any Go module dependencies detected from the generated source files.
//
// Example output (sbom.json):
//
//	{
//	  "bomFormat": "CycloneDX",
//	  "specVersion": "1.4",
//	  "serialNumber": "urn:uuid:<uuid>",
//	  "version": 1,
//	  "metadata": { "timestamp": "...", "component": { ... } },
//	  "components": [ ... ]
//	}
package sbom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ─── CycloneDX types ──────────────────────────────────────────────────────────

// SBOM is a minimal CycloneDX 1.4 bill of materials document.
type SBOM struct {
	BOMFormat    string      `json:"bomFormat"`
	SpecVersion  string      `json:"specVersion"`
	SerialNumber string      `json:"serialNumber"`
	Version      int         `json:"version"`
	Metadata     Metadata    `json:"metadata"`
	Components   []Component `json:"components"`
}

// Metadata contains the document creation metadata and the root component.
type Metadata struct {
	Timestamp string    `json:"timestamp"`
	Component Component `json:"component"`
}

// Component describes a single software component.
type Component struct {
	Type        string       `json:"type"`
	BOMRef      string       `json:"bom-ref,omitempty"`
	Name        string       `json:"name"`
	Version     string       `json:"version,omitempty"`
	Description string       `json:"description,omitempty"`
	Language    string       `json:"language,omitempty"`
	Hashes      []Hash       `json:"hashes,omitempty"`
	Licenses    []LicenseRef `json:"licenses,omitempty"`
	Properties  []Property   `json:"properties,omitempty"`
}

// Hash is an algorithm+value pair.
type Hash struct {
	Alg     string `json:"alg"`
	Content string `json:"content"`
}

// LicenseRef wraps a license expression.
type LicenseRef struct {
	License LicenseExpr `json:"license"`
}

// LicenseExpr holds a SPDX license expression.
type LicenseExpr struct {
	ID string `json:"id"`
}

// Property is a key-value pair for custom metadata.
type Property struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ─── Builder input ────────────────────────────────────────────────────────────

// BuildInfo is passed by the builder pipeline to generate the SBOM.
type BuildInfo struct {
	// SkillName is the canonical name of the skill.
	SkillName string
	// SkillDescription is a human-readable skill description.
	SkillDescription string
	// Version is the skill version string (e.g. "1.0.0" or git short SHA).
	Version string
	// Language is the primary language of the skill (e.g. "Go", "Python").
	Language string
	// Files maps relative file paths to their source contents.
	Files map[string]string
	// FileHashes maps relative file paths to their SHA-256 hex digests.
	FileHashes map[string]string
	// ProposalID is the proposal that produced this skill.
	ProposalID string
}

// ─── Generation ───────────────────────────────────────────────────────────────

// Generate produces an SBOM for the given skill build.
func Generate(info BuildInfo) *SBOM {
	now := time.Now().UTC().Format(time.RFC3339)

	root := Component{
		Type:        "application",
		BOMRef:      "skill/" + info.SkillName,
		Name:        info.SkillName,
		Version:     info.Version,
		Description: info.SkillDescription,
		Language:    info.Language,
		Properties: []Property{
			{Name: "aegisclaw:proposal_id", Value: info.ProposalID},
			{Name: "aegisclaw:generated_at", Value: now},
		},
	}

	// Compute aggregate hash over all file hashes.
	if len(info.FileHashes) > 0 {
		h := sha256.New()
		for _, path := range sortedKeys(info.FileHashes) {
			fmt.Fprintf(h, "%s=%s\n", path, info.FileHashes[path])
		}
		root.Hashes = []Hash{{Alg: "SHA-256", Content: hex.EncodeToString(h.Sum(nil))}}
	}

	components := extractGoModules(info.Files)

	return &SBOM{
		BOMFormat:    "CycloneDX",
		SpecVersion:  "1.4",
		SerialNumber: "urn:uuid:" + uuid.New().String(),
		Version:      1,
		Metadata: Metadata{
			Timestamp: now,
			Component: root,
		},
		Components: components,
	}
}

// Write serialises the SBOM as indented JSON to destDir/sbom.json.
// Returns the path of the written file.
func Write(destDir string, s *SBOM) (string, error) {
	if err := os.MkdirAll(destDir, 0700); err != nil {
		return "", fmt.Errorf("create sbom dir: %w", err)
	}
	path := filepath.Join(destDir, "sbom.json")
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal sbom: %w", err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		return "", fmt.Errorf("write sbom: %w", err)
	}
	return path, nil
}

// Read deserialises an SBOM from a JSON file.
func Read(path string) (*SBOM, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read sbom: %w", err)
	}
	var s SBOM
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse sbom: %w", err)
	}
	return &s, nil
}

// ─── Go module extractor ──────────────────────────────────────────────────────

// goImportRe matches a single import path inside a Go source file.
var goImportRe = regexp.MustCompile(`"([a-zA-Z0-9._/\-]+)"`)

// goModRequireRe matches a require line in go.mod.
var goModRequireRe = regexp.MustCompile(`^\s*([a-zA-Z0-9._/\-]+)\s+v([0-9][^\s]*)`)

// extractGoModules scans the provided source files for Go module dependencies.
// It tries go.mod first for accurate version data, then falls back to parsing
// import statements in .go files.
func extractGoModules(files map[string]string) []Component {
	if gomod, ok := files["go.mod"]; ok {
		return parseGoMod(gomod)
	}

	// Fall back to scanning imports.
	seen := make(map[string]bool)
	var comps []Component
	for path, content := range files {
		if !strings.HasSuffix(path, ".go") {
			continue
		}
		for _, match := range goImportRe.FindAllStringSubmatch(content, -1) {
			pkg := match[1]
			if !strings.Contains(pkg, ".") {
				continue
			}
			parts := strings.SplitN(pkg, "/", 4)
			n := 3
			if n > len(parts) {
				n = len(parts)
			}
			mod := strings.Join(parts[:n], "/")
			if !seen[mod] {
				seen[mod] = true
				comps = append(comps, Component{
					Type:    "library",
					BOMRef:  "pkg/" + mod,
					Name:    mod,
					Version: "unknown",
					Properties: []Property{
						{Name: "aegisclaw:source", Value: "import-scan"},
					},
				})
			}
		}
	}
	return comps
}

func parseGoMod(content string) []Component {
	var comps []Component
	inRequire := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "require (" {
			inRequire = true
			continue
		}
		if inRequire && trimmed == ")" {
			inRequire = false
			continue
		}
		if strings.HasPrefix(trimmed, "require ") {
			trimmed = strings.TrimPrefix(trimmed, "require ")
		}
		if inRequire || strings.HasPrefix(line, "require ") {
			if m := goModRequireRe.FindStringSubmatch(trimmed); len(m) == 3 {
				comps = append(comps, Component{
					Type:    "library",
					BOMRef:  "pkg/" + m[1],
					Name:    m[1],
					Version: m[2],
					Properties: []Property{
						{Name: "aegisclaw:source", Value: "go.mod"},
					},
				})
			}
		}
	}
	return comps
}

// sortedKeys returns the keys of m in lexicographic order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
