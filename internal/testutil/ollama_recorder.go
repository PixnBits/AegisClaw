package testutil

import (
	"net/http"
	"os"
	"testing"

	"gopkg.in/dnaeon/go-vcr.v2/recorder"
)

// OllamaRecorder is a helper for recording/replaying LLM calls in tests.
type OllamaRecorder struct {
	rec *recorder.Recorder
}

// Test constants for Ollama
const (
	TestOllamaSeed        = int64(42)
	TestOllamaTemperature = 0.7
)

// RecordingOllama returns true if we should record Ollama interactions
func RecordingOllama() bool {
	return os.Getenv("RECORD_OLLAMA") == "1"
}

// OllamaCassetteExists checks if a cassette file exists
func OllamaCassetteExists(cassetteName string) bool {
	_, err := os.Stat(cassetteName)
	return err == nil
}

// Float64 returns a pointer to a float64 value
func Float64(v float64) *float64 {
	return &v
}

func NewOllamaRecorder(t *testing.T, cassetteName string) *OllamaRecorder {
	r, err := recorder.NewAsMode(cassetteName, recorder.ModeRecording, http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Stop() })

	return &OllamaRecorder{rec: r}
}

// NewOllamaRecorderClient returns an HTTP client that records/replays
func NewOllamaRecorderClient(t *testing.T, cassetteName string) *http.Client {
	rec := NewOllamaRecorder(t, cassetteName)
	return &http.Client{Transport: rec.rec}
}

// TODO: Add methods as needed for tests
