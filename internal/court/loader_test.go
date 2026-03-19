package court

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap/zaptest"
	"gopkg.in/yaml.v3"
)

func TestLoadPersonas(t *testing.T) {
	logger := zaptest.NewLogger(t)
	dir := t.TempDir()

	// Write test persona files
	personas := map[string]string{
		"ciso.yaml": `name: CISO
role: security
system_prompt: "Review security"
models:
  - test-model
weight: 0.3
`,
		"coder.yaml": `name: Coder
role: code_quality
system_prompt: "Review code"
models:
  - test-model
weight: 0.3
`,
	}

	for name, content := range personas {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	loaded, err := LoadPersonas(dir, logger)
	if err != nil {
		t.Fatalf("LoadPersonas failed: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("expected 2 personas, got %d", len(loaded))
	}
}

func TestLoadPersonasEmptyDir(t *testing.T) {
	logger := zaptest.NewLogger(t)
	dir := t.TempDir()

	_, err := LoadPersonas(dir, logger)
	if err == nil {
		t.Error("expected error for empty directory")
	}
}

func TestLoadPersonasInvalidYAML(t *testing.T) {
	logger := zaptest.NewLogger(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("{{invalid"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadPersonas(dir, logger)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadPersonasMissingFields(t *testing.T) {
	logger := zaptest.NewLogger(t)
	dir := t.TempDir()

	// Persona missing required name
	content := `role: security
system_prompt: "Review"
models:
  - test
weight: 0.5
`
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadPersonas(dir, logger)
	if err == nil {
		t.Error("expected error for persona with missing name")
	}
}

func TestLoadPersonasSkipsNonYAML(t *testing.T) {
	logger := zaptest.NewLogger(t)
	dir := t.TempDir()

	// Write a non-YAML file (should be skipped)
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a persona"), 0600); err != nil {
		t.Fatal(err)
	}

	// Write a valid YAML persona
	content := `name: CISO
role: security
system_prompt: "Review"
models:
  - test-model
weight: 0.5
`
	if err := os.WriteFile(filepath.Join(dir, "ciso.yaml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadPersonas(dir, logger)
	if err != nil {
		t.Fatalf("LoadPersonas failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("expected 1 persona (skip non-YAML), got %d", len(loaded))
	}
}

func TestLoadPersonasEmptyPath(t *testing.T) {
	logger := zaptest.NewLogger(t)
	_, err := LoadPersonas("", logger)
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestLoadPersonasYMLExtension(t *testing.T) {
	logger := zaptest.NewLogger(t)
	dir := t.TempDir()

	content := `name: Tester
role: test_coverage
system_prompt: "Review tests"
models:
  - test-model
weight: 0.5
`
	if err := os.WriteFile(filepath.Join(dir, "tester.yml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadPersonas(dir, logger)
	if err != nil {
		t.Fatalf("LoadPersonas failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("expected 1 persona with .yml extension, got %d", len(loaded))
	}
}

func TestPersonaValidation(t *testing.T) {
	tests := []struct {
		name    string
		persona Persona
		wantErr bool
	}{
		{
			"valid",
			Persona{Name: "CISO", Role: "sec", SystemPrompt: "check", Models: []string{"m"}, Weight: 0.5},
			false,
		},
		{
			"empty name",
			Persona{Role: "sec", SystemPrompt: "check", Models: []string{"m"}, Weight: 0.5},
			true,
		},
		{
			"empty role",
			Persona{Name: "CISO", SystemPrompt: "check", Models: []string{"m"}, Weight: 0.5},
			true,
		},
		{
			"empty prompt",
			Persona{Name: "CISO", Role: "sec", Models: []string{"m"}, Weight: 0.5},
			true,
		},
		{
			"no models",
			Persona{Name: "CISO", Role: "sec", SystemPrompt: "check", Weight: 0.5},
			true,
		},
		{
			"weight zero",
			Persona{Name: "CISO", Role: "sec", SystemPrompt: "check", Models: []string{"m"}, Weight: 0},
			true,
		},
		{
			"weight > 1",
			Persona{Name: "CISO", Role: "sec", SystemPrompt: "check", Models: []string{"m"}, Weight: 1.5},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.persona.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultPersonas(t *testing.T) {
	// Verify all default persona YAML strings are valid
	for name, content := range defaultPersonas {
		var p Persona
		if err := parseYAMLString(content, &p); err != nil {
			t.Errorf("default persona %s has invalid YAML: %v", name, err)
			continue
		}
		if err := p.Validate(); err != nil {
			t.Errorf("default persona %s is invalid: %v", name, err)
		}
	}
}

func parseYAMLString(s string, v interface{}) error {
	return yaml.Unmarshal([]byte(s), v)
}
