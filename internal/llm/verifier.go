package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ConsensusThreshold is the default minimum agreement ratio required
// for cross-model verification to pass without escalation.
const ConsensusThreshold = 0.66

// VerificationLevel classifies the security sensitivity of a decision.
type VerificationLevel int

const (
	// VerifyStandard requires a single model response.
	VerifyStandard VerificationLevel = iota
	// VerifyCritical requires cross-model consensus (CISO-level, kernel-impacting).
	VerifyCritical
)

// String returns the human-readable verification level name.
func (vl VerificationLevel) String() string {
	switch vl {
	case VerifyStandard:
		return "standard"
	case VerifyCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// VerificationRequest describes a request to verify across multiple models.
type VerificationRequest struct {
	// Persona is the persona name making the decision.
	Persona string
	// Prompt is the user/system prompt to send.
	Prompt string
	// System is the system prompt prepended to the request.
	System string
	// Models are the models to query for cross-verification.
	Models []string
	// Temperature for the LLM calls.
	Temperature float64
	// Schema defines the expected output structure.
	Schema *OutputSchema
	// MaxTokens per model call.
	MaxTokens int
	// Level determines the verification strictness.
	Level VerificationLevel
	// ConsensusThreshold overrides the default threshold (0.0 uses default).
	ConsensusThreshold float64
}

// ModelResponse captures a single model's response in cross-verification.
type ModelResponse struct {
	Model    string         `json:"model"`
	Parsed   map[string]any `json:"parsed"`
	Raw      string         `json:"raw"`
	Duration time.Duration  `json:"duration"`
	Error    string         `json:"error,omitempty"`
}

// VerificationResult is the outcome of cross-model verification.
type VerificationResult struct {
	// Consensus indicates whether the models agreed above the threshold.
	Consensus bool `json:"consensus"`
	// Agreement is the ratio of models that agree with the majority.
	Agreement float64 `json:"agreement"`
	// MajorityResponse is the response from the majority of models.
	MajorityResponse map[string]any `json:"majority_response"`
	// Responses contains all individual model responses.
	Responses []ModelResponse `json:"responses"`
	// Discrepancies lists fields where at least one model disagreed.
	Discrepancies []Discrepancy `json:"discrepancies,omitempty"`
	// EscalateToHuman is true if consensus was not reached.
	EscalateToHuman bool `json:"escalate_to_human"`
	// Duration is the total verification time.
	Duration time.Duration `json:"duration"`
}

// Discrepancy records a field-level disagreement between models.
type Discrepancy struct {
	Field  string            `json:"field"`
	Values []DiscrepantValue `json:"values"`
}

// DiscrepantValue records a model's value for a discrepant field.
type DiscrepantValue struct {
	Model string `json:"model"`
	Value any    `json:"value"`
}

// VerifierConfig configures the cross-model verification engine.
type VerifierConfig struct {
	ConsensusThreshold float64
	MaxRetries         int
	TemperatureDecay   float64
}

// Verifier runs cross-model verification for critical decisions.
type Verifier struct {
	enforcer *Enforcer
	logger   *zap.Logger
	config   VerifierConfig
}

// NewVerifier creates a cross-model verification engine.
func NewVerifier(enforcer *Enforcer, logger *zap.Logger, cfg VerifierConfig) *Verifier {
	if cfg.ConsensusThreshold <= 0 {
		cfg.ConsensusThreshold = ConsensusThreshold
	}
	return &Verifier{
		enforcer: enforcer,
		logger:   logger,
		config:   cfg,
	}
}

// Verify runs the verification request against multiple models and checks consensus.
func (v *Verifier) Verify(ctx context.Context, req VerificationRequest) (*VerificationResult, error) {
	if len(req.Models) == 0 {
		return nil, fmt.Errorf("at least one model is required")
	}
	if req.Schema == nil {
		return nil, fmt.Errorf("output schema is required")
	}

	start := time.Now()

	// For standard verification, just use the first model
	if req.Level == VerifyStandard {
		return v.verifySingle(ctx, req, start)
	}

	// Critical: run on all models in parallel
	if len(req.Models) < 2 {
		return nil, fmt.Errorf("critical verification requires at least 2 models, got %d", len(req.Models))
	}

	return v.verifyMulti(ctx, req, start)
}

func (v *Verifier) verifySingle(ctx context.Context, req VerificationRequest, start time.Time) (*VerificationResult, error) {
	resp, err := v.enforcer.Generate(ctx, EnforcedRequest{
		Model:       req.Models[0],
		System:      req.System,
		Prompt:      req.Prompt,
		Temperature: req.Temperature,
		Schema:      req.Schema,
		MaxTokens:   req.MaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("single model verification failed: %w", err)
	}

	return &VerificationResult{
		Consensus:        true,
		Agreement:        1.0,
		MajorityResponse: resp.Parsed,
		Responses: []ModelResponse{{
			Model:  req.Models[0],
			Parsed: resp.Parsed,
			Raw:    resp.Raw,
		}},
		Duration: time.Since(start),
	}, nil
}

func (v *Verifier) verifyMulti(ctx context.Context, req VerificationRequest, start time.Time) (*VerificationResult, error) {
	type modelResult struct {
		resp *EnforcedResponse
		err  error
		dur  time.Duration
	}

	results := make([]modelResult, len(req.Models))
	var wg sync.WaitGroup

	for i, model := range req.Models {
		wg.Add(1)
		go func(idx int, m string) {
			defer wg.Done()
			mStart := time.Now()
			resp, err := v.enforcer.Generate(ctx, EnforcedRequest{
				Model:       m,
				System:      req.System,
				Prompt:      req.Prompt,
				Temperature: req.Temperature,
				Schema:      req.Schema,
				MaxTokens:   req.MaxTokens,
			})
			results[idx] = modelResult{resp: resp, err: err, dur: time.Since(mStart)}
		}(i, model)
	}
	wg.Wait()

	// Collect successful responses
	var responses []ModelResponse
	for i, r := range results {
		mr := ModelResponse{
			Model:    req.Models[i],
			Duration: r.dur,
		}
		if r.err != nil {
			mr.Error = r.err.Error()
			v.logger.Error("model verification failed",
				zap.String("model", req.Models[i]),
				zap.Error(r.err),
			)
		} else {
			mr.Parsed = r.resp.Parsed
			mr.Raw = r.resp.Raw
		}
		responses = append(responses, mr)
	}

	// Count successful responses
	var successful []ModelResponse
	for _, r := range responses {
		if r.Error == "" {
			successful = append(successful, r)
		}
	}

	if len(successful) == 0 {
		return nil, fmt.Errorf("all models failed during cross-verification")
	}

	// Find discrepancies and compute consensus
	discrepancies := v.findDiscrepancies(successful, req.Schema)
	majority := v.findMajority(successful, req.Schema)
	agreement := v.computeAgreement(successful, majority, req.Schema)

	threshold := req.ConsensusThreshold
	if threshold <= 0 {
		threshold = v.config.ConsensusThreshold
	}

	consensus := agreement >= threshold
	escalate := !consensus

	if escalate {
		v.logger.Warn("cross-model verification failed consensus",
			zap.String("persona", req.Persona),
			zap.Float64("agreement", agreement),
			zap.Float64("threshold", threshold),
			zap.Int("discrepancies", len(discrepancies)),
		)
	} else {
		v.logger.Info("cross-model verification passed",
			zap.String("persona", req.Persona),
			zap.Float64("agreement", agreement),
			zap.Int("models", len(successful)),
		)
	}

	// Log discrepancies as structured JSON for audit
	if len(discrepancies) > 0 {
		discJSON, _ := json.Marshal(discrepancies)
		v.logger.Info("verification discrepancies",
			zap.String("persona", req.Persona),
			zap.String("discrepancies", string(discJSON)),
		)
	}

	return &VerificationResult{
		Consensus:        consensus,
		Agreement:        agreement,
		MajorityResponse: majority,
		Responses:        responses,
		Discrepancies:    discrepancies,
		EscalateToHuman:  escalate,
		Duration:         time.Since(start),
	}, nil
}

// findDiscrepancies compares all successful responses and returns field-level disagreements.
func (v *Verifier) findDiscrepancies(responses []ModelResponse, schema *OutputSchema) []Discrepancy {
	var discrepancies []Discrepancy

	for _, field := range schema.Fields {
		values := make([]DiscrepantValue, 0, len(responses))
		for _, r := range responses {
			val, ok := r.Parsed[field.Name]
			if !ok {
				continue
			}
			values = append(values, DiscrepantValue{Model: r.Model, Value: val})
		}

		if len(values) < 2 {
			continue
		}

		if !allEqual(values) {
			discrepancies = append(discrepancies, Discrepancy{
				Field:  field.Name,
				Values: values,
			})
		}
	}

	return discrepancies
}

// findMajority determines the majority response by counting matching values per field.
func (v *Verifier) findMajority(responses []ModelResponse, schema *OutputSchema) map[string]any {
	if len(responses) == 0 {
		return nil
	}
	if len(responses) == 1 {
		return responses[0].Parsed
	}

	majority := make(map[string]any)

	for _, field := range schema.Fields {
		// Count each unique value
		type valCount struct {
			val   any
			count int
		}
		var counts []valCount

		for _, r := range responses {
			val, ok := r.Parsed[field.Name]
			if !ok {
				continue
			}
			found := false
			for i, vc := range counts {
				if valuesMatch(vc.val, val) {
					counts[i].count++
					found = true
					break
				}
			}
			if !found {
				counts = append(counts, valCount{val: val, count: 1})
			}
		}

		if len(counts) > 0 {
			best := counts[0]
			for _, vc := range counts[1:] {
				if vc.count > best.count {
					best = vc
				}
			}
			majority[field.Name] = best.val
		}
	}

	return majority
}

// computeAgreement calculates what fraction of models agree with the majority on all fields.
func (v *Verifier) computeAgreement(responses []ModelResponse, majority map[string]any, schema *OutputSchema) float64 {
	if len(responses) <= 1 {
		return 1.0
	}

	totalFields := 0
	agreeCount := 0

	for _, field := range schema.Fields {
		mVal, ok := majority[field.Name]
		if !ok {
			continue
		}
		totalFields++
		for _, r := range responses {
			rVal, ok := r.Parsed[field.Name]
			if !ok {
				continue
			}
			if valuesMatch(mVal, rVal) {
				agreeCount++
			}
		}
	}

	if totalFields == 0 {
		return 1.0
	}

	maxPossible := totalFields * len(responses)
	return float64(agreeCount) / float64(maxPossible)
}

// allEqual checks if all discrepant values are equal.
func allEqual(values []DiscrepantValue) bool {
	if len(values) < 2 {
		return true
	}
	first := values[0].Value
	for _, v := range values[1:] {
		if !valuesMatch(first, v.Value) {
			return false
		}
	}
	return true
}

// valuesMatch compares two JSON-decoded values for equality.
func valuesMatch(a, b any) bool {
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		return ok && math.Abs(av-bv) < 0.001
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	default:
		// For complex types (arrays, objects), use JSON serialization
		aj, _ := json.Marshal(a)
		bj, _ := json.Marshal(b)
		return string(aj) == string(bj)
	}
}

// AuditEntry creates a JSON-serializable record of a verification result
// suitable for appending to the audit log.
func (vr *VerificationResult) AuditEntry(persona string, level VerificationLevel) json.RawMessage {
	entry := map[string]any{
		"type":              "cross_model_verification",
		"persona":           persona,
		"level":             level.String(),
		"consensus":         vr.Consensus,
		"agreement":         vr.Agreement,
		"escalate_to_human": vr.EscalateToHuman,
		"model_count":       len(vr.Responses),
		"discrepancy_count": len(vr.Discrepancies),
		"duration_ms":       vr.Duration.Milliseconds(),
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
	}

	if len(vr.Discrepancies) > 0 {
		entry["discrepancies"] = vr.Discrepancies
	}

	models := make([]string, 0, len(vr.Responses))
	for _, r := range vr.Responses {
		models = append(models, r.Model)
	}
	entry["models"] = models

	raw, _ := json.Marshal(entry)
	return raw
}
