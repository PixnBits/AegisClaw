package llm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// RouteMode controls how models are selected for a persona.
type RouteMode string

const (
	// RouteModePrimary uses the first available model only.
	RouteModePrimary RouteMode = "primary"
	// RouteModeFallback tries models in order until one succeeds.
	RouteModeFallback RouteMode = "fallback"
	// RouteModeEnsemble runs all models and aggregates results.
	RouteModeEnsemble RouteMode = "ensemble"
)

// PersonaRoute defines the model routing configuration for a single persona.
type PersonaRoute struct {
	Persona      string    `yaml:"persona"`
	Models       []string  `yaml:"models"`
	Temperature  float64   `yaml:"temperature"`
	Mode         RouteMode `yaml:"mode"`
	OutputSchema string    `yaml:"output_schema,omitempty"`
	MaxTokens    int       `yaml:"max_tokens,omitempty"`
}

// Validate checks the route configuration.
func (r *PersonaRoute) Validate() error {
	if r.Persona == "" {
		return fmt.Errorf("persona name is required")
	}
	if len(r.Models) == 0 {
		return fmt.Errorf("at least one model is required for persona %q", r.Persona)
	}
	switch r.Mode {
	case RouteModePrimary, RouteModeFallback, RouteModeEnsemble, "":
	default:
		return fmt.Errorf("invalid route mode %q for persona %q", r.Mode, r.Persona)
	}
	if r.Temperature < 0 || r.Temperature > 2 {
		return fmt.Errorf("temperature must be between 0 and 2, got %f", r.Temperature)
	}
	return nil
}

// RouterConfig is the top-level YAML structure for persona routing.
type RouterConfig struct {
	DefaultTemperature float64        `yaml:"default_temperature"`
	DefaultMode        RouteMode      `yaml:"default_mode"`
	DefaultMaxTokens   int            `yaml:"default_max_tokens"`
	Routes             []PersonaRoute `yaml:"routes"`
}

// Router resolves which model(s) and parameters to use for a given persona.
type Router struct {
	routes             map[string]*PersonaRoute
	defaultTemperature float64
	defaultMode        RouteMode
	defaultMaxTokens   int
}

// NewRouter creates a router from a RouterConfig.
func NewRouter(cfg RouterConfig) (*Router, error) {
	r := &Router{
		routes:             make(map[string]*PersonaRoute),
		defaultTemperature: cfg.DefaultTemperature,
		defaultMode:        cfg.DefaultMode,
		defaultMaxTokens:   cfg.DefaultMaxTokens,
	}

	if r.defaultTemperature == 0 {
		r.defaultTemperature = 0.7
	}
	if r.defaultMode == "" {
		r.defaultMode = RouteModeFallback
	}
	if r.defaultMaxTokens == 0 {
		r.defaultMaxTokens = 4096
	}

	for i := range cfg.Routes {
		route := &cfg.Routes[i]
		if err := route.Validate(); err != nil {
			return nil, fmt.Errorf("invalid route: %w", err)
		}
		r.routes[route.Persona] = route
	}

	return r, nil
}

// LoadRouter loads routing config from a YAML file.
func LoadRouter(path string) (*Router, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read router config %s: %w", path, err)
	}

	var cfg RouterConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse router config: %w", err)
	}

	return NewRouter(cfg)
}

// LoadRouterFromDir builds a router from persona YAML files in a directory,
// using each persona's models list and sensible defaults.
func LoadRouterFromDir(dir string) (*Router, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read persona dir %s: %w", dir, err)
	}

	cfg := RouterConfig{
		DefaultTemperature: 0.7,
		DefaultMode:        RouteModeFallback,
		DefaultMaxTokens:   4096,
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read persona file %s: %w", e.Name(), err)
		}

		var persona struct {
			Name         string   `yaml:"name"`
			Models       []string `yaml:"models"`
			OutputSchema string   `yaml:"output_schema"`
		}
		if err := yaml.Unmarshal(data, &persona); err != nil {
			return nil, fmt.Errorf("parse persona file %s: %w", e.Name(), err)
		}
		if persona.Name == "" || len(persona.Models) == 0 {
			continue
		}

		cfg.Routes = append(cfg.Routes, PersonaRoute{
			Persona:      persona.Name,
			Models:       persona.Models,
			Temperature:  0.7,
			Mode:         RouteModeFallback,
			OutputSchema: persona.OutputSchema,
		})
	}

	return NewRouter(cfg)
}

// Resolve returns the routing configuration for the given persona.
// If no explicit route exists, returns a default with the given fallback models.
func (r *Router) Resolve(persona string) ResolvedRoute {
	if route, ok := r.routes[persona]; ok {
		temp := route.Temperature
		if temp == 0 {
			temp = r.defaultTemperature
		}
		mode := route.Mode
		if mode == "" {
			mode = r.defaultMode
		}
		maxTokens := route.MaxTokens
		if maxTokens == 0 {
			maxTokens = r.defaultMaxTokens
		}
		return ResolvedRoute{
			Persona:      persona,
			Models:       route.Models,
			Temperature:  temp,
			Mode:         mode,
			OutputSchema: route.OutputSchema,
			MaxTokens:    maxTokens,
		}
	}

	return ResolvedRoute{
		Persona:     persona,
		Temperature: r.defaultTemperature,
		Mode:        r.defaultMode,
		MaxTokens:   r.defaultMaxTokens,
	}
}

// Personas returns all configured persona names.
func (r *Router) Personas() []string {
	names := make([]string, 0, len(r.routes))
	for name := range r.routes {
		names = append(names, name)
	}
	return names
}

// HasPersona returns true if the router has a route for the given persona.
func (r *Router) HasPersona(persona string) bool {
	_, ok := r.routes[persona]
	return ok
}

// ResolvedRoute is the fully resolved configuration for invoking models for a persona.
type ResolvedRoute struct {
	Persona      string
	Models       []string
	Temperature  float64
	Mode         RouteMode
	OutputSchema string
	MaxTokens    int
}

// PrimaryModel returns the first model in the list. Returns empty if no models.
func (rr ResolvedRoute) PrimaryModel() string {
	if len(rr.Models) == 0 {
		return ""
	}
	return rr.Models[0]
}

// EnsembleModels returns all models when in ensemble mode, or just the primary otherwise.
func (rr ResolvedRoute) EnsembleModels() []string {
	if rr.Mode == RouteModeEnsemble {
		return rr.Models
	}
	if len(rr.Models) == 0 {
		return nil
	}
	return rr.Models[:1]
}
