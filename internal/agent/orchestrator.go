package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// OrchestratorState is a minimal persisted state record for a long-lived
// supervisor loop. It tracks active task identifiers and worker references.
type OrchestratorState struct {
	UpdatedAt   time.Time         `json:"updated_at"`
	CurrentTask string            `json:"current_task,omitempty"`
	Workers     map[string]string `json:"workers,omitempty"` // worker_id -> status
}

// LoadState reads orchestrator state from disk if present.
func LoadState(path string) (*OrchestratorState, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &OrchestratorState{
				UpdatedAt: time.Now().UTC(),
				Workers:   map[string]string{},
			}, nil
		}
		return nil, fmt.Errorf("read orchestrator state: %w", err)
	}

	var st OrchestratorState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("parse orchestrator state: %w", err)
	}
	if st.Workers == nil {
		st.Workers = map[string]string{}
	}
	return &st, nil
}

// SaveState writes orchestrator state atomically.
func SaveState(path string, st *OrchestratorState) error {
	if st == nil {
		return fmt.Errorf("orchestrator state is required")
	}
	st.UpdatedAt = time.Now().UTC()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal orchestrator state: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return fmt.Errorf("write temp orchestrator state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit orchestrator state: %w", err)
	}
	return nil
}
