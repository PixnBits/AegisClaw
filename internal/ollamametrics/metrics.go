package ollamametrics

import (
	"encoding/json"
	"log"
)

// ParseGenerateMetrics parses raw Ollama /api/generate JSON and returns model + counters.
func ParseGenerateMetrics(raw []byte) (model string, counts map[string]float64, err error) {
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", nil, err
	}
	if m, ok := out["model"].(string); ok {
		model = m
	}
	getF := func(k string) float64 {
		if v, ok := out[k]; ok && v != nil {
			switch vv := v.(type) {
			case float64:
				return vv
			case int:
				return float64(vv)
			case int64:
				return float64(vv)
			case json.Number:
				f, _ := vv.Float64()
				return f
			}
		}
		return 0
	}
	counts = map[string]float64{
		"prompt_eval_count":    getF("prompt_eval_count"),
		"eval_count":           getF("eval_count"),
		"prompt_eval_duration": getF("prompt_eval_duration"),
		"eval_duration":        getF("eval_duration"),
		"total_duration":       getF("total_duration"),
		"load_duration":        getF("load_duration"),
	}
	return model, counts, nil
}

// ExtractResponseText extracts the .response text or returns the input as-is.
func ExtractResponseText(raw []byte) string {
	text := string(raw)
	var out map[string]interface{}
	if json.Unmarshal(raw, &out) == nil {
		if r, ok := out["response"].(string); ok && r != "" {
			text = r
		}
	}
	return text
}

// LogLLMMetrics emits the required structured metrics line.
func LogLLMMetrics(model string, promptLen int, counts map[string]float64) {
	log.Printf("LLM metrics: model=%s prompt_len=%d prompt_eval_count=%.0f eval_count=%.0f prompt_eval_duration=%.0f eval_duration=%.0f total_duration=%.0f load_duration=%.0f",
		model, promptLen,
		counts["prompt_eval_count"], counts["eval_count"],
		counts["prompt_eval_duration"], counts["eval_duration"],
		counts["total_duration"], counts["load_duration"])
}
