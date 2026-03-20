package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
)

func TestVerificationLevelString(t *testing.T) {
	tests := []struct {
		vl   VerificationLevel
		want string
	}{
		{VerifyStandard, "standard"},
		{VerifyCritical, "critical"},
		{VerificationLevel(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.vl.String(); got != tt.want {
			t.Errorf("VerificationLevel(%d).String() = %q, want %q", tt.vl, got, tt.want)
		}
	}
}

func newTestVerifier(handler http.Handler) (*Verifier, *httptest.Server) {
	srv := httptest.NewServer(handler)
	client := NewClient(ClientConfig{Endpoint: srv.URL})
	logger := zap.NewNop()
	enforcer := NewEnforcer(client, logger, EnforcerConfig{MaxRetries: 1, TemperatureDecay: 0.1})
	verifier := NewVerifier(enforcer, logger, VerifierConfig{ConsensusThreshold: 0.66})
	return verifier, srv
}

func TestVerifySingleModel(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(GenerateResponse{
			Response: `{"verdict": "approve", "risk_score": 2.0, "evidence": ["safe"]}`,
			Done:     true,
		})
	})

	verifier, srv := newTestVerifier(handler)
	defer srv.Close()

	result, err := verifier.Verify(context.Background(), VerificationRequest{
		Persona:     "Tester",
		Prompt:      "review this",
		Models:      []string{"model-a"},
		Temperature: 0.5,
		Schema:      ReviewSchema,
		Level:       VerifyStandard,
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.Consensus {
		t.Error("expected consensus for single model")
	}
	if result.Agreement != 1.0 {
		t.Errorf("expected 1.0 agreement, got %f", result.Agreement)
	}
	if len(result.Responses) != 1 {
		t.Errorf("expected 1 response, got %d", len(result.Responses))
	}
}

func TestVerifyCriticalConsensus(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// All models agree
		json.NewEncoder(w).Encode(GenerateResponse{
			Response: `{"verdict": "approve", "risk_score": 3.0, "evidence": ["ok"]}`,
			Done:     true,
		})
	})

	verifier, srv := newTestVerifier(handler)
	defer srv.Close()

	result, err := verifier.Verify(context.Background(), VerificationRequest{
		Persona:     "CISO",
		Prompt:      "review kernel change",
		Models:      []string{"model-a", "model-b"},
		Temperature: 0.3,
		Schema:      ReviewSchema,
		Level:       VerifyCritical,
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !result.Consensus {
		t.Error("expected consensus when models agree")
	}
	if result.EscalateToHuman {
		t.Error("should not escalate when consensus reached")
	}
	if len(result.Responses) != 2 {
		t.Errorf("expected 2 responses, got %d", len(result.Responses))
	}
}

func TestVerifyCriticalDisagreement(t *testing.T) {
	var callNum atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callNum.Add(1)
		if n <= 1 { // First call (could be retry too, but first model)
			json.NewEncoder(w).Encode(GenerateResponse{
				Response: `{"verdict": "approve", "risk_score": 2.0, "evidence": ["safe"]}`,
				Done:     true,
			})
		} else {
			json.NewEncoder(w).Encode(GenerateResponse{
				Response: `{"verdict": "reject", "risk_score": 8.0, "evidence": ["dangerous"]}`,
				Done:     true,
			})
		}
	})

	verifier, srv := newTestVerifier(handler)
	defer srv.Close()

	result, err := verifier.Verify(context.Background(), VerificationRequest{
		Persona:     "CISO",
		Prompt:      "review kernel change",
		Models:      []string{"model-a", "model-b"},
		Temperature: 0.3,
		Schema:      ReviewSchema,
		Level:       VerifyCritical,
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.Consensus {
		t.Error("expected no consensus when models disagree")
	}
	if !result.EscalateToHuman {
		t.Error("expected escalation when no consensus")
	}
	if len(result.Discrepancies) == 0 {
		t.Error("expected discrepancies")
	}
}

func TestVerifyNoModels(t *testing.T) {
	verifier := NewVerifier(nil, zap.NewNop(), VerifierConfig{})
	_, err := verifier.Verify(context.Background(), VerificationRequest{
		Models: nil,
		Schema: ReviewSchema,
	})
	if err == nil {
		t.Error("expected error for no models")
	}
}

func TestVerifyNoSchema(t *testing.T) {
	verifier := NewVerifier(nil, zap.NewNop(), VerifierConfig{})
	_, err := verifier.Verify(context.Background(), VerificationRequest{
		Models: []string{"m"},
	})
	if err == nil {
		t.Error("expected error for no schema")
	}
}

func TestVerifyCriticalOneModel(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(GenerateResponse{
			Response: `{"verdict": "approve", "risk_score": 1.0, "evidence": ["ok"]}`,
			Done:     true,
		})
	})

	verifier, srv := newTestVerifier(handler)
	defer srv.Close()

	_, err := verifier.Verify(context.Background(), VerificationRequest{
		Models: []string{"only-one"},
		Schema: ReviewSchema,
		Level:  VerifyCritical,
	})
	if err == nil {
		t.Error("expected error for critical verification with only 1 model")
	}
}

func TestValuesMatch(t *testing.T) {
	tests := []struct {
		a, b any
		want bool
	}{
		{"approve", "approve", true},
		{"approve", "reject", false},
		{3.0, 3.0, true},
		{3.0, 3.01, false},
		{3.0, 3.0005, true},
		{true, true, true},
		{true, false, false},
		{[]any{"a"}, []any{"a"}, true},
		{[]any{"a"}, []any{"b"}, false},
		{"str", 123.0, false},
	}
	for _, tt := range tests {
		got := valuesMatch(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("valuesMatch(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestAllEqual(t *testing.T) {
	if !allEqual(nil) {
		t.Error("nil should be equal")
	}
	if !allEqual([]DiscrepantValue{{Value: "a"}}) {
		t.Error("single value should be equal")
	}
	if !allEqual([]DiscrepantValue{{Value: "a"}, {Value: "a"}}) {
		t.Error("same values should be equal")
	}
	if allEqual([]DiscrepantValue{{Value: "a"}, {Value: "b"}}) {
		t.Error("different values should not be equal")
	}
}

func TestAuditEntry(t *testing.T) {
	vr := &VerificationResult{
		Consensus: true,
		Agreement: 1.0,
		Responses: []ModelResponse{
			{Model: "model-a"},
			{Model: "model-b"},
		},
	}

	entry := vr.AuditEntry("CISO", VerifyCritical)
	var parsed map[string]any
	if err := json.Unmarshal(entry, &parsed); err != nil {
		t.Fatalf("AuditEntry JSON: %v", err)
	}
	if parsed["type"] != "cross_model_verification" {
		t.Errorf("unexpected type: %v", parsed["type"])
	}
	if parsed["persona"] != "CISO" {
		t.Errorf("unexpected persona: %v", parsed["persona"])
	}
	if parsed["level"] != "critical" {
		t.Errorf("unexpected level: %v", parsed["level"])
	}
	if parsed["consensus"] != true {
		t.Error("expected consensus=true")
	}
}

func TestFindDiscrepancies(t *testing.T) {
	v := NewVerifier(nil, zap.NewNop(), VerifierConfig{})
	responses := []ModelResponse{
		{Model: "a", Parsed: map[string]any{"verdict": "approve", "risk_score": 2.0, "evidence": []any{"ok"}}},
		{Model: "b", Parsed: map[string]any{"verdict": "reject", "risk_score": 8.0, "evidence": []any{"bad"}}},
	}

	discs := v.findDiscrepancies(responses, ReviewSchema)
	if len(discs) == 0 {
		t.Error("expected discrepancies")
	}

	// Check that verdict is among discrepancies
	foundVerdict := false
	for _, d := range discs {
		if d.Field == "verdict" {
			foundVerdict = true
			if len(d.Values) != 2 {
				t.Errorf("expected 2 values for verdict discrepancy, got %d", len(d.Values))
			}
		}
	}
	if !foundVerdict {
		t.Error("expected verdict discrepancy")
	}
}

func TestFindMajority(t *testing.T) {
	v := NewVerifier(nil, zap.NewNop(), VerifierConfig{})
	responses := []ModelResponse{
		{Model: "a", Parsed: map[string]any{"verdict": "approve", "risk_score": 2.0, "evidence": []any{"ok"}}},
		{Model: "b", Parsed: map[string]any{"verdict": "approve", "risk_score": 3.0, "evidence": []any{"ok"}}},
		{Model: "c", Parsed: map[string]any{"verdict": "reject", "risk_score": 8.0, "evidence": []any{"bad"}}},
	}

	majority := v.findMajority(responses, ReviewSchema)
	if majority["verdict"] != "approve" {
		t.Errorf("expected approve majority, got %v", majority["verdict"])
	}
}

func TestComputeAgreement(t *testing.T) {
	v := NewVerifier(nil, zap.NewNop(), VerifierConfig{})

	// All agree
	allAgree := []ModelResponse{
		{Model: "a", Parsed: map[string]any{"verdict": "approve"}},
		{Model: "b", Parsed: map[string]any{"verdict": "approve"}},
	}
	schema := &OutputSchema{Fields: []SchemaField{{Name: "verdict", Type: FieldString, Required: true}}}
	majority := map[string]any{"verdict": "approve"}

	agreement := v.computeAgreement(allAgree, majority, schema)
	if agreement != 1.0 {
		t.Errorf("expected 1.0 agreement, got %f", agreement)
	}

	// Half agree
	halfAgree := []ModelResponse{
		{Model: "a", Parsed: map[string]any{"verdict": "approve"}},
		{Model: "b", Parsed: map[string]any{"verdict": "reject"}},
	}
	agreement = v.computeAgreement(halfAgree, majority, schema)
	if agreement != 0.5 {
		t.Errorf("expected 0.5 agreement, got %f", agreement)
	}

	// Single response
	single := []ModelResponse{
		{Model: "a", Parsed: map[string]any{"verdict": "approve"}},
	}
	agreement = v.computeAgreement(single, majority, schema)
	if agreement != 1.0 {
		t.Errorf("expected 1.0 for single, got %f", agreement)
	}
}

func TestVerifyAllModelsFail(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	verifier, srv := newTestVerifier(handler)
	defer srv.Close()

	_, err := verifier.Verify(context.Background(), VerificationRequest{
		Persona:     "CISO",
		Prompt:      "test",
		Models:      []string{"a", "b"},
		Temperature: 0.3,
		Schema:      ReviewSchema,
		Level:       VerifyCritical,
	})
	if err == nil {
		t.Error("expected error when all models fail")
	}
}
