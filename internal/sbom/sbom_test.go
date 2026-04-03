package sbom_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/sbom"
)

func TestGenerate_Basic(t *testing.T) {
	info := sbom.BuildInfo{
		SkillName:        "greeter",
		SkillDescription: "A simple greeting skill",
		Version:          "1.0.0",
		Language:         "Go",
		ProposalID:       "prop-abc123",
		Files: map[string]string{
			"main.go": `package main
import (
	"fmt"
	"github.com/spf13/cobra"
)
func main() { fmt.Println("hello") }`,
		},
		FileHashes: map[string]string{
			"main.go": "deadbeefdeadbeef",
		},
	}

	s := sbom.Generate(info)

	if s.BOMFormat != "CycloneDX" {
		t.Errorf("BOMFormat = %q, want CycloneDX", s.BOMFormat)
	}
	if s.SpecVersion != "1.4" {
		t.Errorf("SpecVersion = %q, want 1.4", s.SpecVersion)
	}
	if !strings.HasPrefix(s.SerialNumber, "urn:uuid:") {
		t.Errorf("SerialNumber = %q, want urn:uuid:...", s.SerialNumber)
	}
	if s.Metadata.Component.Name != "greeter" {
		t.Errorf("root component name = %q, want greeter", s.Metadata.Component.Name)
	}
	if s.Metadata.Component.Version != "1.0.0" {
		t.Errorf("root component version = %q, want 1.0.0", s.Metadata.Component.Version)
	}
	if len(s.Metadata.Component.Hashes) == 0 {
		t.Error("expected aggregate hash on root component")
	}
	// Should detect github.com/spf13/cobra from import scan.
	found := false
	for _, c := range s.Components {
		if strings.Contains(c.Name, "spf13") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected github.com/spf13/cobra in components, got %v", s.Components)
	}
}

func TestGenerate_GoMod(t *testing.T) {
	info := sbom.BuildInfo{
		SkillName:  "myskill",
		ProposalID: "prop-xyz",
		Files: map[string]string{
			"go.mod": `module myskill

go 1.21

require (
	github.com/google/uuid v1.6.0
	go.uber.org/zap v1.27.1
)
`,
		},
	}
	s := sbom.Generate(info)

	names := make(map[string]bool)
	for _, c := range s.Components {
		names[c.Name] = true
	}
	if !names["github.com/google/uuid"] {
		t.Errorf("expected github.com/google/uuid in components")
	}
	if !names["go.uber.org/zap"] {
		t.Errorf("expected go.uber.org/zap in components")
	}
	// Version should be parsed correctly.
	for _, c := range s.Components {
		if c.Name == "github.com/google/uuid" && c.Version != "1.6.0" {
			t.Errorf("uuid version = %q, want 1.6.0", c.Version)
		}
	}
}

func TestWriteRead_RoundTrip(t *testing.T) {
	info := sbom.BuildInfo{
		SkillName:  "round-trip",
		ProposalID: "prop-rt",
		Files:      map[string]string{"main.go": "package main"},
	}
	s := sbom.Generate(info)

	dir := t.TempDir()
	path, err := sbom.Write(dir, s)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !strings.HasSuffix(path, "sbom.json") {
		t.Errorf("expected sbom.json path, got %q", path)
	}

	// Verify file is valid JSON.
	raw, err := os.ReadFile(filepath.Join(dir, "sbom.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var check map[string]interface{}
	if err := json.Unmarshal(raw, &check); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}

	// Round-trip read.
	s2, err := sbom.Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s2.BOMFormat != s.BOMFormat {
		t.Errorf("BOMFormat mismatch after round-trip")
	}
	if s2.Metadata.Component.Name != s.Metadata.Component.Name {
		t.Errorf("component name mismatch after round-trip")
	}
}

func TestGenerate_EmptyFiles(t *testing.T) {
	s := sbom.Generate(sbom.BuildInfo{SkillName: "empty", ProposalID: "p"})
	if s == nil {
		t.Fatal("expected non-nil SBOM")
	}
	if s.BOMFormat != "CycloneDX" {
		t.Errorf("BOMFormat = %q", s.BOMFormat)
	}
	if s.SpecVersion != "1.4" {
		t.Errorf("SpecVersion = %q, want 1.4", s.SpecVersion)
	}
	if len(s.Components) != 0 {
		t.Errorf("expected 0 components for empty files, got %d", len(s.Components))
	}
}
