package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// MaxEnforcementRetries is the maximum number of retries for structured output parsing.
const MaxEnforcementRetries = 3

// TemperatureDecay is subtracted from the temperature on each retry to encourage
// more deterministic output.
const TemperatureDecay = 0.1

// SchemaField describes an expected field in a JSON response.
type SchemaField struct {
	Name     string
	Required bool
	Type     FieldType
}

// FieldType represents the expected JSON type for a schema field.
type FieldType int

const (
	FieldString FieldType = iota
	FieldNumber
	FieldBool
	FieldArray
	FieldObject
)

// String returns the human-readable field type name.
func (ft FieldType) String() string {
	switch ft {
	case FieldString:
		return "string"
	case FieldNumber:
		return "number"
	case FieldBool:
		return "bool"
	case FieldArray:
		return "array"
	case FieldObject:
		return "object"
	default:
		return "unknown"
	}
}

// OutputSchema defines the expected JSON structure for an LLM response.
type OutputSchema struct {
	Fields []SchemaField
}

// Validate checks a parsed JSON map against the schema.
func (s *OutputSchema) Validate(data map[string]any) error {
	for _, f := range s.Fields {
		val, exists := data[f.Name]
		if !exists {
			if f.Required {
				return fmt.Errorf("missing required field %q", f.Name)
			}
			continue
		}
		if err := checkType(f, val); err != nil {
			return err
		}
	}
	return nil
}

func checkType(f SchemaField, val any) error {
	switch f.Type {
	case FieldString:
		if _, ok := val.(string); !ok {
			return fmt.Errorf("field %q: expected string, got %T", f.Name, val)
		}
	case FieldNumber:
		if _, ok := val.(float64); !ok {
			return fmt.Errorf("field %q: expected number, got %T", f.Name, val)
		}
	case FieldBool:
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("field %q: expected bool, got %T", f.Name, val)
		}
	case FieldArray:
		if _, ok := val.([]any); !ok {
			return fmt.Errorf("field %q: expected array, got %T", f.Name, val)
		}
	case FieldObject:
		if _, ok := val.(map[string]any); !ok {
			return fmt.Errorf("field %q: expected object, got %T", f.Name, val)
		}
	}
	return nil
}

// ReviewSchema is the expected output schema for Court reviewer responses.
var ReviewSchema = &OutputSchema{
	Fields: []SchemaField{
		{Name: "verdict", Required: true, Type: FieldString},
		{Name: "risk_score", Required: true, Type: FieldNumber},
		{Name: "evidence", Required: true, Type: FieldArray},
		{Name: "questions", Required: false, Type: FieldArray},
		{Name: "comments", Required: false, Type: FieldString},
	},
}

// CodeGenSchema is the expected output schema for code generation responses.
var CodeGenSchema = &OutputSchema{
	Fields: []SchemaField{
		{Name: "files", Required: true, Type: FieldObject},
		{Name: "reasoning", Required: false, Type: FieldString},
	},
}

// EnforcerConfig configures the structured output enforcer.
type EnforcerConfig struct {
	MaxRetries       int
	TemperatureDecay float64
}

// Enforcer wraps LLM calls with JSON schema validation and retry logic.
type Enforcer struct {
	client *Client
	logger *zap.Logger
	config EnforcerConfig
}

// NewEnforcer creates a new structured output enforcer.
func NewEnforcer(client *Client, logger *zap.Logger, cfg EnforcerConfig) *Enforcer {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = MaxEnforcementRetries
	}
	if cfg.TemperatureDecay <= 0 {
		cfg.TemperatureDecay = TemperatureDecay
	}
	return &Enforcer{
		client: client,
		logger: logger,
		config: cfg,
	}
}

// EnforcedRequest describes a structured output request to the LLM.
type EnforcedRequest struct {
	Model       string
	System      string
	Prompt      string
	Temperature float64
	Schema      *OutputSchema
	MaxTokens   int
}

// EnforcedResponse contains the validated structured output.
type EnforcedResponse struct {
	Raw      string
	Parsed   map[string]any
	Model    string
	Retries  int
	LastTemp float64
}

// Generate sends a prompt to the LLM and enforces structured JSON output.
// It retries up to MaxRetries times, lowering temperature on each retry.
func (e *Enforcer) Generate(ctx context.Context, req EnforcedRequest) (*EnforcedResponse, error) {
	if req.Schema == nil {
		return nil, fmt.Errorf("output schema is required")
	}
	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}

	temp := req.Temperature
	var lastErr error

	for attempt := 0; attempt <= e.config.MaxRetries; attempt++ {
		if attempt > 0 {
			temp = temp - e.config.TemperatureDecay
			if temp < 0 {
				temp = 0
			}
			e.logger.Warn("retrying structured output",
				zap.Int("attempt", attempt),
				zap.Float64("temperature", temp),
				zap.Error(lastErr),
			)
		}

		genResp, err := e.client.Generate(ctx, GenerateRequest{
			Model:       req.Model,
			System:      req.System,
			Prompt:      req.Prompt,
			Temperature: temp,
			Format:      "json",
			Options:     e.buildOptions(req.MaxTokens),
		})
		if err != nil {
			lastErr = fmt.Errorf("generate call failed: %w", err)
			continue
		}

		parsed, err := e.parseAndValidate(genResp.Response, req.Schema)
		if err != nil {
			lastErr = fmt.Errorf("output validation failed: %w", err)
			continue
		}

		return &EnforcedResponse{
			Raw:      genResp.Response,
			Parsed:   parsed,
			Model:    req.Model,
			Retries:  attempt,
			LastTemp: temp,
		}, nil
	}

	return nil, fmt.Errorf("structured output enforcement failed after %d retries: %w", e.config.MaxRetries, lastErr)
}

// Chat sends a chat request and enforces structured JSON output.
func (e *Enforcer) Chat(ctx context.Context, model string, messages []ChatMessage, temp float64, schema *OutputSchema, maxTokens int) (*EnforcedResponse, error) {
	if schema == nil {
		return nil, fmt.Errorf("output schema is required")
	}
	if model == "" {
		return nil, fmt.Errorf("model is required")
	}

	var lastErr error

	for attempt := 0; attempt <= e.config.MaxRetries; attempt++ {
		if attempt > 0 {
			temp = temp - e.config.TemperatureDecay
			if temp < 0 {
				temp = 0
			}
			e.logger.Warn("retrying structured chat output",
				zap.Int("attempt", attempt),
				zap.Float64("temperature", temp),
				zap.Error(lastErr),
			)
		}

		chatResp, err := e.client.Chat(ctx, ChatRequest{
			Model:       model,
			Messages:    messages,
			Temperature: temp,
			Format:      "json",
			Options:     e.buildOptions(maxTokens),
		})
		if err != nil {
			lastErr = fmt.Errorf("chat call failed: %w", err)
			continue
		}

		parsed, err := e.parseAndValidate(chatResp.Message.Content, schema)
		if err != nil {
			lastErr = fmt.Errorf("chat output validation failed: %w", err)
			continue
		}

		return &EnforcedResponse{
			Raw:      chatResp.Message.Content,
			Parsed:   parsed,
			Model:    model,
			Retries:  attempt,
			LastTemp: temp,
		}, nil
	}

	return nil, fmt.Errorf("structured chat output enforcement failed after %d retries: %w", e.config.MaxRetries, lastErr)
}

// parseAndValidate extracts JSON from the LLM response and validates it.
func (e *Enforcer) parseAndValidate(raw string, schema *OutputSchema) (map[string]any, error) {
	raw = strings.TrimSpace(raw)

	// Try direct parse first
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		// Try to extract JSON from markdown code blocks
		extracted := extractJSON(raw)
		if extracted == "" {
			return nil, fmt.Errorf("response is not valid JSON: %w", err)
		}
		if err := json.Unmarshal([]byte(extracted), &data); err != nil {
			return nil, fmt.Errorf("extracted JSON is not valid: %w", err)
		}
	}

	if err := schema.Validate(data); err != nil {
		return nil, err
	}

	return data, nil
}

// extractJSON attempts to find a JSON object in a string, handling common LLM
// patterns like wrapping in markdown code blocks.
func extractJSON(s string) string {
	// Try to find ```json ... ``` blocks
	if idx := strings.Index(s, "```json"); idx >= 0 {
		start := idx + len("```json")
		end := strings.Index(s[start:], "```")
		if end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}

	// Try to find ``` ... ``` blocks
	if idx := strings.Index(s, "```"); idx >= 0 {
		start := idx + len("```")
		end := strings.Index(s[start:], "```")
		if end >= 0 {
			candidate := strings.TrimSpace(s[start : start+end])
			if len(candidate) > 0 && candidate[0] == '{' {
				return candidate
			}
		}
	}

	// Try to find the first { ... } block
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}

	return ""
}

func (e *Enforcer) buildOptions(maxTokens int) map[string]any {
	if maxTokens <= 0 {
		return nil
	}
	return map[string]any{
		"num_predict": maxTokens,
	}
}

// ParseOutputSchema parses a JSON schema string (as used in persona configs) into
// an OutputSchema. The input is expected to be a JSON object with field names as keys
// and type descriptors as values.
func ParseOutputSchema(raw string) (*OutputSchema, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty schema string")
	}

	var schemaMap map[string]any
	if err := json.Unmarshal([]byte(raw), &schemaMap); err != nil {
		return nil, fmt.Errorf("invalid schema JSON: %w", err)
	}

	var fields []SchemaField
	for name, typeDesc := range schemaMap {
		ft, required, err := parseFieldDesc(typeDesc)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		fields = append(fields, SchemaField{
			Name:     name,
			Type:     ft,
			Required: required,
		})
	}

	return &OutputSchema{Fields: fields}, nil
}

func parseFieldDesc(v any) (FieldType, bool, error) {
	switch desc := v.(type) {
	case string:
		return typeFromString(desc)
	case float64:
		return FieldNumber, true, nil
	case bool:
		return FieldBool, true, nil
	case []any:
		return FieldArray, true, nil
	case map[string]any:
		return FieldObject, true, nil
	default:
		return 0, false, fmt.Errorf("unsupported type descriptor: %T", v)
	}
}

func typeFromString(s string) (FieldType, bool, error) {
	s = strings.ToLower(strings.TrimSpace(s))

	// Check for optional prefix
	required := true
	if strings.HasPrefix(s, "optional ") {
		required = false
		s = strings.TrimPrefix(s, "optional ")
	}
	if strings.HasPrefix(s, "?") {
		required = false
		s = strings.TrimPrefix(s, "?")
	}

	switch s {
	case "string":
		return FieldString, required, nil
	case "number", "float", "float64", "int":
		return FieldNumber, required, nil
	case "bool", "boolean":
		return FieldBool, required, nil
	case "array", "[]string", "[string]", "list":
		return FieldArray, required, nil
	case "object", "map", "{}":
		return FieldObject, required, nil
	default:
		// Treat unrecognized descriptors as string by default
		return FieldString, required, nil
	}
}
