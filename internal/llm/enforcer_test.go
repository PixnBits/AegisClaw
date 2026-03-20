package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
)

func TestOutputSchemaValidate(t *testing.T) {
	schema := &OutputSchema{
		Fields: []SchemaField{
			{Name: "verdict", Required: true, Type: FieldString},
			{Name: "risk_score", Required: true, Type: FieldNumber},
			{Name: "evidence", Required: true, Type: FieldArray},
			{Name: "comments", Required: false, Type: FieldString},
		},
	}

	valid := map[string]any{
		"verdict":    "approve",
		"risk_score": 2.5,
		"evidence":   []any{"safe code"},
	}
	if err := schema.Validate(valid); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}

	// Missing required field
	missing := map[string]any{
		"verdict": "approve",
	}
	if err := schema.Validate(missing); err == nil {
		t.Error("expected error for missing required field")
	}

	// Wrong type
	wrongType := map[string]any{
		"verdict":    123,
		"risk_score": 2.5,
		"evidence":   []any{"x"},
	}
	if err := schema.Validate(wrongType); err == nil {
		t.Error("expected error for wrong type")
	}

	// Optional field missing is OK
	noComments := map[string]any{
		"verdict":    "approve",
		"risk_score": 1.0,
		"evidence":   []any{"ok"},
	}
	if err := schema.Validate(noComments); err != nil {
		t.Errorf("optional field missing should be OK: %v", err)
	}
}

func TestFieldTypeString(t *testing.T) {
	tests := []struct {
		ft   FieldType
		want string
	}{
		{FieldString, "string"},
		{FieldNumber, "number"},
		{FieldBool, "bool"},
		{FieldArray, "array"},
		{FieldObject, "object"},
		{FieldType(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.ft.String(); got != tt.want {
			t.Errorf("FieldType(%d).String() = %q, want %q", tt.ft, got, tt.want)
		}
	}
}

func TestReviewSchemaFields(t *testing.T) {
	if len(ReviewSchema.Fields) != 5 {
		t.Errorf("expected 5 fields in ReviewSchema, got %d", len(ReviewSchema.Fields))
	}
}

func TestCodeGenSchemaFields(t *testing.T) {
	if len(CodeGenSchema.Fields) != 2 {
		t.Errorf("expected 2 fields in CodeGenSchema, got %d", len(CodeGenSchema.Fields))
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain json",
			input: `{"key": "value"}`,
			want:  `{"key": "value"}`,
		},
		{
			name:  "markdown json block",
			input: "Some text\n```json\n{\"key\": \"value\"}\n```\nMore text",
			want:  `{"key": "value"}`,
		},
		{
			name:  "markdown plain block",
			input: "Text\n```\n{\"key\": \"value\"}\n```\nEnd",
			want:  `{"key": "value"}`,
		},
		{
			name:  "embedded json",
			input: "Here is the result: {\"a\": 1} done",
			want:  `{"a": 1}`,
		},
		{
			name:  "no json",
			input: "just text with no braces",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if got != tt.want {
				t.Errorf("extractJSON(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseOutputSchema(t *testing.T) {
	schema, err := ParseOutputSchema(`{"verdict": "string", "risk_score": 0.0, "evidence": ["string"]}`)
	if err != nil {
		t.Fatalf("ParseOutputSchema: %v", err)
	}
	if len(schema.Fields) != 3 {
		t.Errorf("expected 3 fields, got %d", len(schema.Fields))
	}
}

func TestParseOutputSchemaEmpty(t *testing.T) {
	_, err := ParseOutputSchema("")
	if err == nil {
		t.Error("expected error for empty schema")
	}
}

func TestParseOutputSchemaInvalid(t *testing.T) {
	_, err := ParseOutputSchema("{invalid")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseOutputSchemaOptionalField(t *testing.T) {
	schema, err := ParseOutputSchema(`{"name": "string", "age": "optional number"}`)
	if err != nil {
		t.Fatalf("ParseOutputSchema: %v", err)
	}

	var nameField, ageField *SchemaField
	for i := range schema.Fields {
		switch schema.Fields[i].Name {
		case "name":
			nameField = &schema.Fields[i]
		case "age":
			ageField = &schema.Fields[i]
		}
	}
	if nameField == nil || !nameField.Required {
		t.Error("name should be required")
	}
	if ageField == nil || ageField.Required {
		t.Error("age should be optional")
	}
	if ageField != nil && ageField.Type != FieldNumber {
		t.Errorf("age should be number, got %s", ageField.Type)
	}
}

func newTestEnforcer(handler http.Handler) (*Enforcer, *httptest.Server) {
	srv := httptest.NewServer(handler)
	client := NewClient(ClientConfig{Endpoint: srv.URL})
	logger := zap.NewNop()
	enforcer := NewEnforcer(client, logger, EnforcerConfig{
		MaxRetries:       2,
		TemperatureDecay: 0.1,
	})
	return enforcer, srv
}

func TestEnforcerGenerateSuccess(t *testing.T) {
	validJSON := `{"verdict": "approve", "risk_score": 3.0, "evidence": ["looks good"]}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(GenerateResponse{
			Response: validJSON,
			Done:     true,
		})
	})

	enforcer, srv := newTestEnforcer(handler)
	defer srv.Close()

	resp, err := enforcer.Generate(context.Background(), EnforcedRequest{
		Model:       "test-model",
		Prompt:      "review this",
		Temperature: 0.5,
		Schema:      ReviewSchema,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Retries != 0 {
		t.Errorf("expected 0 retries, got %d", resp.Retries)
	}
	if resp.Parsed["verdict"] != "approve" {
		t.Errorf("expected approve, got %v", resp.Parsed["verdict"])
	}
}

func TestEnforcerGenerateRetry(t *testing.T) {
	var callCount atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n <= 2 {
			// Return invalid JSON first two times
			json.NewEncoder(w).Encode(GenerateResponse{
				Response: `{"invalid": true}`,
				Done:     true,
			})
			return
		}
		// Third call returns valid
		json.NewEncoder(w).Encode(GenerateResponse{
			Response: `{"verdict": "reject", "risk_score": 8.0, "evidence": ["risky"]}`,
			Done:     true,
		})
	})

	enforcer, srv := newTestEnforcer(handler)
	defer srv.Close()

	resp, err := enforcer.Generate(context.Background(), EnforcedRequest{
		Model:       "test-model",
		Prompt:      "review this",
		Temperature: 0.7,
		Schema:      ReviewSchema,
	})
	if err != nil {
		t.Fatalf("Generate with retry: %v", err)
	}
	if resp.Retries != 2 {
		t.Errorf("expected 2 retries, got %d", resp.Retries)
	}
}

func TestEnforcerGenerateExhausted(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(GenerateResponse{
			Response: "not json at all",
			Done:     true,
		})
	})

	enforcer, srv := newTestEnforcer(handler)
	defer srv.Close()

	_, err := enforcer.Generate(context.Background(), EnforcedRequest{
		Model:       "test-model",
		Prompt:      "review this",
		Temperature: 0.5,
		Schema:      ReviewSchema,
	})
	if err == nil {
		t.Error("expected error after exhausting retries")
	}
}

func TestEnforcerGenerateNoSchema(t *testing.T) {
	enforcer := NewEnforcer(NewClient(ClientConfig{}), zap.NewNop(), EnforcerConfig{})
	_, err := enforcer.Generate(context.Background(), EnforcedRequest{
		Model:  "m",
		Prompt: "p",
	})
	if err == nil {
		t.Error("expected error for nil schema")
	}
}

func TestEnforcerGenerateNoModel(t *testing.T) {
	enforcer := NewEnforcer(NewClient(ClientConfig{}), zap.NewNop(), EnforcerConfig{})
	_, err := enforcer.Generate(context.Background(), EnforcedRequest{
		Prompt: "p",
		Schema: ReviewSchema,
	})
	if err == nil {
		t.Error("expected error for empty model")
	}
}

func TestEnforcerChatSuccess(t *testing.T) {
	validJSON := `{"files": {"main.go": "package main"}, "reasoning": "done"}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ChatResponse{
			Message: ChatMessage{Role: "assistant", Content: validJSON},
			Done:    true,
		})
	})

	enforcer, srv := newTestEnforcer(handler)
	defer srv.Close()

	msgs := []ChatMessage{{Role: "user", Content: "generate code"}}
	resp, err := enforcer.Chat(context.Background(), "test-model", msgs, 0.5, CodeGenSchema, 4096)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Retries != 0 {
		t.Errorf("expected 0 retries, got %d", resp.Retries)
	}
	files, ok := resp.Parsed["files"].(map[string]any)
	if !ok {
		t.Fatal("expected files map")
	}
	if files["main.go"] != "package main" {
		t.Errorf("unexpected file content: %v", files["main.go"])
	}
}

func TestEnforcerChatRetry(t *testing.T) {
	var callCount atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			json.NewEncoder(w).Encode(ChatResponse{
				Message: ChatMessage{Role: "assistant", Content: `{"bad": true}`},
				Done:    true,
			})
			return
		}
		json.NewEncoder(w).Encode(ChatResponse{
			Message: ChatMessage{Role: "assistant", Content: `{"files": {"x.go": "code"}}`},
			Done:    true,
		})
	})

	enforcer, srv := newTestEnforcer(handler)
	defer srv.Close()

	msgs := []ChatMessage{{Role: "user", Content: "gen"}}
	resp, err := enforcer.Chat(context.Background(), "model", msgs, 0.5, CodeGenSchema, 0)
	if err != nil {
		t.Fatalf("Chat with retry: %v", err)
	}
	if resp.Retries != 1 {
		t.Errorf("expected 1 retry, got %d", resp.Retries)
	}
}

func TestEnforcerChatNoSchema(t *testing.T) {
	enforcer := NewEnforcer(NewClient(ClientConfig{}), zap.NewNop(), EnforcerConfig{})
	_, err := enforcer.Chat(context.Background(), "m", nil, 0.5, nil, 0)
	if err == nil {
		t.Error("expected error for nil schema")
	}
}

func TestEnforcerChatNoModel(t *testing.T) {
	enforcer := NewEnforcer(NewClient(ClientConfig{}), zap.NewNop(), EnforcerConfig{})
	_, err := enforcer.Chat(context.Background(), "", nil, 0.5, CodeGenSchema, 0)
	if err == nil {
		t.Error("expected error for empty model")
	}
}

func TestEnforcerGenerateMarkdownWrapped(t *testing.T) {
	wrappedJSON := fmt.Sprintf("```json\n%s\n```", `{"verdict": "approve", "risk_score": 1.0, "evidence": ["clean"]}`)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(GenerateResponse{
			Response: wrappedJSON,
			Done:     true,
		})
	})

	enforcer, srv := newTestEnforcer(handler)
	defer srv.Close()

	resp, err := enforcer.Generate(context.Background(), EnforcedRequest{
		Model:       "model",
		Prompt:      "review",
		Temperature: 0.5,
		Schema:      ReviewSchema,
	})
	if err != nil {
		t.Fatalf("Generate with markdown: %v", err)
	}
	if resp.Parsed["verdict"] != "approve" {
		t.Errorf("expected approve, got %v", resp.Parsed["verdict"])
	}
}

func TestEnforcerTemperatureDecay(t *testing.T) {
	var temps []float64

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GenerateRequest
		json.NewDecoder(r.Body).Decode(&req)
		temps = append(temps, req.Temperature)
		// Always return invalid to force retries
		json.NewEncoder(w).Encode(GenerateResponse{Response: "{}", Done: true})
	})

	enforcer, srv := newTestEnforcer(handler)
	defer srv.Close()

	enforcer.Generate(context.Background(), EnforcedRequest{
		Model:       "model",
		Prompt:      "test",
		Temperature: 0.7,
		Schema:      ReviewSchema,
	})

	if len(temps) != 3 { // initial + 2 retries
		t.Fatalf("expected 3 calls, got %d", len(temps))
	}
	if temps[0] != 0.7 {
		t.Errorf("first call temp should be 0.7, got %f", temps[0])
	}
	if temps[1] != 0.6 {
		t.Errorf("second call temp should be 0.6, got %f", temps[1])
	}
	if temps[2] < 0.49 || temps[2] > 0.51 {
		t.Errorf("third call temp should be ~0.5, got %f", temps[2])
	}
}

func TestEnforcerServerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	})

	enforcer, srv := newTestEnforcer(handler)
	defer srv.Close()

	_, err := enforcer.Generate(context.Background(), EnforcedRequest{
		Model:       "model",
		Prompt:      "test",
		Temperature: 0.5,
		Schema:      ReviewSchema,
	})
	if err == nil {
		t.Error("expected error for server failure")
	}
}

func TestBuildOptionsWithTokens(t *testing.T) {
	e := NewEnforcer(NewClient(ClientConfig{}), zap.NewNop(), EnforcerConfig{})
	opts := e.buildOptions(4096)
	if opts == nil {
		t.Fatal("expected non-nil options")
	}
	if opts["num_predict"] != 4096 {
		t.Errorf("expected num_predict=4096, got %v", opts["num_predict"])
	}

	opts = e.buildOptions(0)
	if opts != nil {
		t.Error("expected nil options for 0 tokens")
	}
}
