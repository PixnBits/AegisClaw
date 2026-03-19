package court

import "fmt"

// Persona defines a court reviewer identity with its system prompt and output schema.
type Persona struct {
	Name         string   `json:"name" yaml:"name"`
	Role         string   `json:"role" yaml:"role"`
	SystemPrompt string   `json:"system_prompt" yaml:"system_prompt"`
	Models       []string `json:"models" yaml:"models"`
	Weight       float64  `json:"weight" yaml:"weight"`
	OutputSchema string   `json:"output_schema" yaml:"output_schema"`
}

// Validate checks that the persona has all required fields.
func (p *Persona) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("persona name is required")
	}
	if p.Role == "" {
		return fmt.Errorf("persona role is required")
	}
	if p.SystemPrompt == "" {
		return fmt.Errorf("persona system prompt is required")
	}
	if len(p.Models) == 0 {
		return fmt.Errorf("persona must have at least one model")
	}
	if p.Weight <= 0 || p.Weight > 1 {
		return fmt.Errorf("persona weight must be between 0 and 1, got %f", p.Weight)
	}
	return nil
}
