//go:build inprocesstest
// +build inprocesstest

// Package exec — Ollama cassette recorder
//
// OllamaRecorder provides lightweight record/replay support for AgentFunc
// calls in in-process tests.  It wraps any AgentFunc and intercepts the
// request/response pairs:
//
//   - RECORD mode  (RECORD_OLLAMA=true): passes through to the real AgentFunc
//     and writes every (request, response) pair as a JSON "cassette" file
//     under the directory specified at construction time.
//
//   - REPLAY mode  (default): reads the cassette and replays responses in
//     call-index order, making tests fully deterministic without any LLM.
//
// Cassette filenames follow the pattern: <name>-<call-index>.json
// where <name> is provided to NewOllamaRecorder.
//
// The cassette directory is typically:
//
//	cmd/aegisclaw/testdata/ollama-cassettes/<test-name>/
//
// Usage (replay — default for CI):
//
//	recorder := exec.NewOllamaRecorder(
//	    "testdata/ollama-cassettes/my-test", // cassette dir
//	    "my-test",                            // cassette name
//	    nil,                                  // real agentFn (not needed in replay)
//	)
//	executor := exec.NewInProcessExecutor(recorder.AgentFunc())
//
// Usage (record new cassettes):
//
//	RECORD_OLLAMA=true go test ./cmd/aegisclaw -tags=inprocesstest -run MyTest
package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const recordOllamaEnv = "RECORD_OLLAMA"

// cassetteTurn stores a single request/response pair for one agent turn.
type cassetteTurn struct {
	Request  AgentTurnRequest  `json:"request"`
	Response AgentTurnResponse `json:"response"`
}

// OllamaRecorder wraps an AgentFunc to record or replay LLM interactions.
//
// In RECORD mode it forwards calls to the real agent and persists the
// request/response pairs as JSON cassette files.
//
// In REPLAY mode it reads back the cassette files and returns the stored
// responses in order, making tests completely deterministic.
type OllamaRecorder struct {
	cassetteDir  string
	name         string
	realAgentFn  AgentFunc // nil is acceptable in replay mode
	mu           sync.Mutex
	callIndex    int
}

// NewOllamaRecorder creates an OllamaRecorder.
//
//   - cassetteDir: directory where cassette JSON files are stored/read.
//   - name: base name for cassette files (used as filename prefix).
//   - realAgentFn: the real AgentFunc called in RECORD mode.  May be nil
//     when RECORD_OLLAMA is not set (replay mode); a panic occurs if record
//     mode is requested without a real AgentFunc.
func NewOllamaRecorder(cassetteDir, name string, realAgentFn AgentFunc) *OllamaRecorder {
	return &OllamaRecorder{
		cassetteDir: cassetteDir,
		name:        name,
		realAgentFn: realAgentFn,
	}
}

// AgentFunc returns an AgentFunc that records or replays based on
// the RECORD_OLLAMA environment variable.
func (o *OllamaRecorder) AgentFunc() AgentFunc {
	if os.Getenv(recordOllamaEnv) == "true" {
		return o.recordingAgentFunc()
	}
	return o.replayAgentFunc()
}

// cassettePath returns the path for a specific call-index cassette file.
func (o *OllamaRecorder) cassettePath(idx int) string {
	return filepath.Join(o.cassetteDir, fmt.Sprintf("%s-%03d.json", o.name, idx))
}

// recordingAgentFunc returns an AgentFunc that forwards to the real agent
// and persists the request/response pair as a cassette file.
func (o *OllamaRecorder) recordingAgentFunc() AgentFunc {
	if o.realAgentFn == nil {
		panic("OllamaRecorder: RECORD_OLLAMA=true requires a non-nil realAgentFn")
	}
	return func(ctx context.Context, req AgentTurnRequest) (AgentTurnResponse, error) {
		resp, err := o.realAgentFn(ctx, req)
		if err != nil {
			return resp, err
		}

		o.mu.Lock()
		idx := o.callIndex
		o.callIndex++
		o.mu.Unlock()

		turn := cassetteTurn{Request: req, Response: resp}
		data, marshalErr := json.MarshalIndent(turn, "", "  ")
		if marshalErr != nil {
			return resp, fmt.Errorf("ollama recorder: marshal cassette: %w", marshalErr)
		}

		path := o.cassettePath(idx)
		if mkdirErr := os.MkdirAll(filepath.Dir(path), 0750); mkdirErr != nil {
			return resp, fmt.Errorf("ollama recorder: mkdir: %w", mkdirErr)
		}
		if writeErr := os.WriteFile(path, data, 0600); writeErr != nil {
			return resp, fmt.Errorf("ollama recorder: write cassette: %w", writeErr)
		}
		return resp, nil
	}
}

// replayAgentFunc returns an AgentFunc that reads cassette files in order.
func (o *OllamaRecorder) replayAgentFunc() AgentFunc {
	return func(_ context.Context, _ AgentTurnRequest) (AgentTurnResponse, error) {
		o.mu.Lock()
		idx := o.callIndex
		o.callIndex++
		o.mu.Unlock()

		path := o.cassettePath(idx)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return AgentTurnResponse{}, fmt.Errorf(
					"ollama recorder: cassette not found: %s\n"+
						"Run with RECORD_OLLAMA=true to record a new cassette.",
					path,
				)
			}
			return AgentTurnResponse{}, fmt.Errorf("ollama recorder: read cassette %s: %w", path, err)
		}

		var turn cassetteTurn
		if unmarshalErr := json.Unmarshal(data, &turn); unmarshalErr != nil {
			return AgentTurnResponse{}, fmt.Errorf("ollama recorder: unmarshal cassette %s: %w", path, unmarshalErr)
		}
		return turn.Response, nil
	}
}
