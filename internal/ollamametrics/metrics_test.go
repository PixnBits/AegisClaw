package ollamametrics

import "testing"

var fixtures = []struct {
	raw            string
	wantModel      string
	wantPE, wantE  float64
}{
	{`{"model":"gemma4:latest","response":"hi","prompt_eval_count":384,"eval_count":192,"total_duration":12428449988}`, "gemma4:latest", 384, 192},
	{`{"model":"gemma4:latest","response":"t","prompt_eval_count":219,"eval_count":580,"total_duration":14007140047}`, "gemma4:latest", 219, 580},
}

func TestParse(t *testing.T) {
	for _, f := range fixtures {
		m, c, err := ParseGenerateMetrics([]byte(f.raw))
		if err != nil {
			t.Fatal(err)
		}
		if m != f.wantModel {
			t.Errorf("model %s", m)
		}
		if c["prompt_eval_count"] != f.wantPE || c["eval_count"] != f.wantE {
			t.Errorf("counts %+v", c)
		}
	}
}

func TestExtract(t *testing.T) {
	got := ExtractResponseText([]byte(`{"response":"the plan text here"}`))
	if got != "the plan text here" {
		t.Error(got)
	}
}

func TestLogNoPanic(t *testing.T) {
	_, c, _ := ParseGenerateMetrics([]byte(fixtures[0].raw))
	LogLLMMetrics("test", 10, c)
}
