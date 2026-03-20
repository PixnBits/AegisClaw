package llm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewRouter(t *testing.T) {
	cfg := RouterConfig{
		DefaultTemperature: 0.5,
		DefaultMode:        RouteModeFallback,
		Routes: []PersonaRoute{
			{Persona: "CISO", Models: []string{"mistral-nemo"}, Temperature: 0.3, Mode: RouteModeEnsemble},
			{Persona: "Tester", Models: []string{"llama3.2:3b"}, Temperature: 0.6},
		},
	}

	r, err := NewRouter(cfg)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	if !r.HasPersona("CISO") {
		t.Error("expected CISO persona")
	}
	if !r.HasPersona("Tester") {
		t.Error("expected Tester persona")
	}
	if r.HasPersona("Unknown") {
		t.Error("unexpected Unknown persona")
	}
}

func TestNewRouterValidation(t *testing.T) {
	_, err := NewRouter(RouterConfig{
		Routes: []PersonaRoute{
			{Persona: "", Models: []string{"m"}},
		},
	})
	if err == nil {
		t.Error("expected error for empty persona name")
	}

	_, err = NewRouter(RouterConfig{
		Routes: []PersonaRoute{
			{Persona: "X", Models: nil},
		},
	})
	if err == nil {
		t.Error("expected error for empty models")
	}

	_, err = NewRouter(RouterConfig{
		Routes: []PersonaRoute{
			{Persona: "X", Models: []string{"m"}, Mode: "invalid"},
		},
	})
	if err == nil {
		t.Error("expected error for invalid mode")
	}

	_, err = NewRouter(RouterConfig{
		Routes: []PersonaRoute{
			{Persona: "X", Models: []string{"m"}, Temperature: 5.0},
		},
	})
	if err == nil {
		t.Error("expected error for out-of-range temperature")
	}
}

func TestRouterResolve(t *testing.T) {
	r, _ := NewRouter(RouterConfig{
		DefaultTemperature: 0.7,
		DefaultMode:        RouteModeFallback,
		DefaultMaxTokens:   4096,
		Routes: []PersonaRoute{
			{Persona: "CISO", Models: []string{"mistral-nemo", "llama3.2:3b"}, Temperature: 0.3, Mode: RouteModeEnsemble, MaxTokens: 8192},
		},
	})

	resolved := r.Resolve("CISO")
	if resolved.Temperature != 0.3 {
		t.Errorf("expected temperature 0.3, got %f", resolved.Temperature)
	}
	if resolved.Mode != RouteModeEnsemble {
		t.Errorf("expected ensemble mode, got %s", resolved.Mode)
	}
	if resolved.MaxTokens != 8192 {
		t.Errorf("expected 8192 max tokens, got %d", resolved.MaxTokens)
	}
	if len(resolved.Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(resolved.Models))
	}
}

func TestRouterResolveDefaults(t *testing.T) {
	r, _ := NewRouter(RouterConfig{
		DefaultTemperature: 0.5,
		DefaultMode:        RouteModeFallback,
		DefaultMaxTokens:   2048,
		Routes: []PersonaRoute{
			{Persona: "X", Models: []string{"m"}},
		},
	})

	// Explicit persona with zero values should get defaults
	resolved := r.Resolve("X")
	if resolved.Temperature != 0.5 {
		t.Errorf("expected default temperature 0.5, got %f", resolved.Temperature)
	}
	if resolved.Mode != RouteModeFallback {
		t.Errorf("expected default mode fallback, got %s", resolved.Mode)
	}
	if resolved.MaxTokens != 2048 {
		t.Errorf("expected default max tokens 2048, got %d", resolved.MaxTokens)
	}
}

func TestRouterResolveUnknown(t *testing.T) {
	r, _ := NewRouter(RouterConfig{
		DefaultTemperature: 0.7,
		DefaultMode:        RouteModePrimary,
		DefaultMaxTokens:   4096,
	})

	resolved := r.Resolve("UnknownPersona")
	if resolved.Persona != "UnknownPersona" {
		t.Errorf("expected persona name, got %q", resolved.Persona)
	}
	if resolved.Temperature != 0.7 {
		t.Errorf("expected default temp, got %f", resolved.Temperature)
	}
	if len(resolved.Models) != 0 {
		t.Errorf("expected no models for unknown, got %d", len(resolved.Models))
	}
}

func TestResolvedRoutePrimaryModel(t *testing.T) {
	rr := ResolvedRoute{Models: []string{"a", "b"}}
	if rr.PrimaryModel() != "a" {
		t.Errorf("expected 'a', got %q", rr.PrimaryModel())
	}

	rr = ResolvedRoute{}
	if rr.PrimaryModel() != "" {
		t.Errorf("expected empty, got %q", rr.PrimaryModel())
	}
}

func TestResolvedRouteEnsembleModels(t *testing.T) {
	rr := ResolvedRoute{Models: []string{"a", "b"}, Mode: RouteModeEnsemble}
	models := rr.EnsembleModels()
	if len(models) != 2 {
		t.Errorf("expected 2 ensemble models, got %d", len(models))
	}

	rr = ResolvedRoute{Models: []string{"a", "b"}, Mode: RouteModeFallback}
	models = rr.EnsembleModels()
	if len(models) != 1 {
		t.Errorf("expected 1 model in fallback mode, got %d", len(models))
	}
	if models[0] != "a" {
		t.Errorf("expected 'a', got %q", models[0])
	}

	rr = ResolvedRoute{Mode: RouteModeEnsemble}
	if rr.EnsembleModels() != nil {
		t.Error("expected nil for empty models")
	}
}

func TestRouterPersonas(t *testing.T) {
	r, _ := NewRouter(RouterConfig{
		Routes: []PersonaRoute{
			{Persona: "A", Models: []string{"m"}},
			{Persona: "B", Models: []string{"m"}},
		},
	})

	personas := r.Personas()
	if len(personas) != 2 {
		t.Errorf("expected 2 personas, got %d", len(personas))
	}
}

func TestLoadRouterFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "personas.yaml")
	os.WriteFile(path, []byte(`
default_temperature: 0.5
default_mode: ensemble
routes:
  - persona: TestPersona
    models:
      - model-a
      - model-b
    temperature: 0.3
    mode: primary
`), 0600)

	r, err := LoadRouter(path)
	if err != nil {
		t.Fatalf("LoadRouter: %v", err)
	}

	resolved := r.Resolve("TestPersona")
	if resolved.Temperature != 0.3 {
		t.Errorf("expected 0.3, got %f", resolved.Temperature)
	}
	if resolved.Mode != RouteModePrimary {
		t.Errorf("expected primary, got %s", resolved.Mode)
	}
}

func TestLoadRouterFromDir(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "ciso.yaml"), []byte(`
name: CISO
models:
  - mistral-nemo
  - llama-3.2-3b
output_schema: '{"verdict":"string"}'
`), 0600)

	os.WriteFile(filepath.Join(dir, "tester.yaml"), []byte(`
name: Tester
models:
  - llama-3.2-3b
`), 0600)

	r, err := LoadRouterFromDir(dir)
	if err != nil {
		t.Fatalf("LoadRouterFromDir: %v", err)
	}

	if !r.HasPersona("CISO") {
		t.Error("expected CISO")
	}
	if !r.HasPersona("Tester") {
		t.Error("expected Tester")
	}

	ciso := r.Resolve("CISO")
	if len(ciso.Models) != 2 {
		t.Errorf("expected 2 CISO models, got %d", len(ciso.Models))
	}
	if ciso.OutputSchema == "" {
		t.Error("expected output schema for CISO")
	}
}

func TestLoadRouterFromDirSkipsNonYAML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a persona"), 0600)
	os.WriteFile(filepath.Join(dir, "valid.yaml"), []byte("name: X\nmodels:\n  - m\n"), 0600)

	r, err := LoadRouterFromDir(dir)
	if err != nil {
		t.Fatalf("LoadRouterFromDir: %v", err)
	}
	if len(r.Personas()) != 1 {
		t.Errorf("expected 1 persona, got %d", len(r.Personas()))
	}
}

func TestPersonaRouteValidate(t *testing.T) {
	valid := PersonaRoute{Persona: "X", Models: []string{"m"}, Temperature: 0.5}
	if err := valid.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Zero temperature is valid (means use default)
	zero := PersonaRoute{Persona: "X", Models: []string{"m"}, Temperature: 0}
	if err := zero.Validate(); err != nil {
		t.Errorf("unexpected error for zero temp: %v", err)
	}
}

func TestLoadRouterMissingFile(t *testing.T) {
	_, err := LoadRouter("/nonexistent/path.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadRouterInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	os.WriteFile(path, []byte("{{{{invalid yaml"), 0600)

	_, err := LoadRouter(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}
