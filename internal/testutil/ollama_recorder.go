package testutil

import (
	"net/http"
	"testing"

	"gopkg.in/dnaeon/go-vcr.v2/recorder"
)

// OllamaRecorder is a helper for recording/replaying LLM calls in tests.
type OllamaRecorder struct {
	rec *recorder.Recorder
}

func NewOllamaRecorder(t *testing.T, cassetteName string) *OllamaRecorder {
	r, err := recorder.NewAsMode(cassetteName, recorder.ModeRecordOnly, http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Stop() })

	return &OllamaRecorder{rec: r}
}

// TODO: Add methods as needed for tests
