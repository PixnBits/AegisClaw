package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/dnaeon/go-vcr/v2/cassette"
	"github.com/dnaeon/go-vcr/v2/recorder"
)

const (
	TestOllamaSeed        int64   = 42
	TestOllamaTemperature float64 = 0
	recordOllamaEnv               = "RECORD_OLLAMA"
)

// RecordingOllama reports whether tests should hit a live Ollama instance and
// refresh the cassette instead of replaying the recorded responses.
func RecordingOllama() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(recordOllamaEnv)), "true")
}

var uuidLikePattern = regexp.MustCompile(
	`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}` +
		`|[0-9a-fA-F]{8}`,
)

// Normalize volatile datetime formats that may appear in tool outputs and
// later be echoed back in the next model request payload.
var dateTimeLikePattern = regexp.MustCompile(
	`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2}|\s+[A-Z]{2,5})?\b`,
)
// Float64 returns a pointer to v for APIs that need to distinguish
// temperature=0 from "temperature not set".
func Float64(v float64) *float64 {
	return &v
}

// NewOllamaRecorderClient returns a go-vcr-backed HTTP client for Ollama.
// Replaying is the default; set RECORD_OLLAMA=true to refresh the cassette.
func NewOllamaRecorderClient(t testing.TB, cassetteName string) *http.Client {
	t.Helper()

	mode := recorder.ModeReplaying
	if RecordingOllama() {
		mode = recorder.ModeRecording
	}

	rec, err := recorder.NewAsMode(cassettePath(t, cassetteName), mode, http.DefaultTransport)
	if err != nil {
		t.Fatalf("new ollama recorder: %v", err)
	}
	rec.SkipRequestLatency = true
	rec.SetReplayableInteractions(true)
	rec.SetMatcher(matchNormalizedOllamaRequest)
	t.Cleanup(func() {
		if err := rec.Stop(); err != nil {
			t.Fatalf("stop ollama recorder: %v", err)
		}
	})

	return &http.Client{Transport: rec}
}

func cassettePath(t testing.TB, cassetteName string) string {
	t.Helper()
	path, err := cassetteBasePath(cassetteName)
	if err != nil {
		t.Fatalf("resolve cassette path: %v", err)
	}
	baseDir := filepath.Dir(path)
	if err := os.MkdirAll(baseDir, 0750); err != nil {
		t.Fatalf("create cassette dir: %v", err)
	}
	return path
}

// OllamaCassetteExists reports whether a recorded cassette is already present
// for the given test case.
func OllamaCassetteExists(cassetteName string) bool {
	path, err := cassetteBasePath(cassetteName)
	if err != nil {
		return false
	}
	_, statErr := os.Stat(path + ".yaml")
	return statErr == nil
}

func cassetteBasePath(cassetteName string) (string, error) {
	_, filePath, _, ok := runtime.Caller(0)
	if !ok {
		return "", os.ErrNotExist
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(filePath)))
	baseDir := filepath.Join(repoRoot, "testdata", "cassettes")
	cleanName := strings.TrimSpace(strings.ReplaceAll(cassetteName, " ", "-"))
	if cleanName == "" {
		cleanName = "ollama"
	}
	return filepath.Join(baseDir, cleanName), nil
}

func matchNormalizedOllamaRequest(req *http.Request, recorded cassette.Request) bool {
	if req.Method != recorded.Method {
		return false
	}
	if req.URL.String() != recorded.URL {
		return false
	}
	return normalizedRequestBody(req) == normalizeJSONBody([]byte(recorded.Body))
}

func normalizedRequestBody(req *http.Request) string {
	if req.Body == nil {
		return ""
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return ""
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return normalizeJSONBody(body)
}

func normalizeJSONBody(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ""
	}
	var decoded any
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return normalizeVolatileString(string(trimmed))
	}
	normalizeJSONValue(decoded)
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return normalizeVolatileString(string(trimmed))
	}
	return string(canonical)
}

func normalizeVolatileString(input string) string {
	normalized := uuidLikePattern.ReplaceAllString(input, "<id>")
	normalized = dateTimeLikePattern.ReplaceAllString(normalized, "<datetime>")
	return normalized
}

func normalizeJSONValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, inner := range typed {
			switch cast := inner.(type) {
			case string:
				typed[key] = normalizeVolatileString(cast)
			default:
				normalizeJSONValue(cast)
			}
		}
	case []any:
		for idx, inner := range typed {
			switch cast := inner.(type) {
			case string:
				typed[idx] = normalizeVolatileString(cast)
			default:
				normalizeJSONValue(cast)
			}
		}
	}
}
