package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// teamRecord is a minimal multi-agent team descriptor persisted on disk.
type teamRecord struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Members   []string `json:"members"`
	CreatedAt string   `json:"created_at"`
}

type teamRegistry struct {
	mu    sync.Mutex
	path  string
	Teams []teamRecord `json:"teams"`
}

func newTeamRegistry(dir string) (*teamRegistry, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("team registry dir: %w", err)
	}
	r := &teamRegistry{path: filepath.Join(dir, "teams.json")}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *teamRegistry) load() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	raw, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		r.Teams = nil
		return nil
	}
	if err != nil {
		return fmt.Errorf("read team registry: %w", err)
	}
	var decoded struct {
		Teams []teamRecord `json:"teams"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("parse team registry: %w", err)
	}
	r.Teams = decoded.Teams
	return nil
}

func (r *teamRegistry) saveLocked() error {
	tmp := r.path + ".tmp"
	payload, err := json.MarshalIndent(struct {
		Teams []teamRecord `json:"teams"`
	}{Teams: r.Teams}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, payload, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

func (r *teamRegistry) list() []teamRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]teamRecord, len(r.Teams))
	copy(out, r.Teams)
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out
}

func (r *teamRegistry) create(name string) (*teamRecord, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("team name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := teamRecord{
		ID:        uuid.New().String(),
		Name:      name,
		Members:   nil,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	r.Teams = append(r.Teams, rec)
	if err := r.saveLocked(); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *teamRegistry) join(teamID, member string) error {
	teamID, member = strings.TrimSpace(teamID), strings.TrimSpace(member)
	if teamID == "" || member == "" {
		return fmt.Errorf("team id and member are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.Teams {
		if r.Teams[i].ID != teamID {
			continue
		}
		for _, m := range r.Teams[i].Members {
			if m == member {
				return nil
			}
		}
		r.Teams[i].Members = append(r.Teams[i].Members, member)
		return r.saveLocked()
	}
	return fmt.Errorf("team %q not found", teamID)
}

func (r *teamRegistry) leave(teamID, member string) error {
	teamID, member = strings.TrimSpace(teamID), strings.TrimSpace(member)
	if teamID == "" || member == "" {
		return fmt.Errorf("team id and member are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.Teams {
		if r.Teams[i].ID != teamID {
			continue
		}
		var kept []string
		for _, m := range r.Teams[i].Members {
			if m != member {
				kept = append(kept, m)
			}
		}
		r.Teams[i].Members = kept
		return r.saveLocked()
	}
	return fmt.Errorf("team %q not found", teamID)
}

func (r *teamRegistry) get(teamID string) (*teamRecord, bool) {
	teamID = strings.TrimSpace(teamID)
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.Teams {
		if r.Teams[i].ID == teamID {
			cp := r.Teams[i]
			return &cp, true
		}
	}
	return nil, false
}

// autonomyRecord tracks delegated autonomy for a chat session.
type autonomyRecord struct {
	SessionID string `json:"session_id"`
	Preset    string `json:"preset,omitempty"`
	Scope     string `json:"scope,omitempty"`
	GrantedAt string `json:"granted_at,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type autonomyRegistry struct {
	mu    sync.Mutex
	path  string
	Items map[string]autonomyRecord `json:"items"`
}

func newAutonomyRegistry(dir string) (*autonomyRegistry, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("autonomy registry dir: %w", err)
	}
	a := &autonomyRegistry{
		path:  filepath.Join(dir, "autonomy.json"),
		Items: make(map[string]autonomyRecord),
	}
	if err := a.load(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *autonomyRegistry) load() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	raw, err := os.ReadFile(a.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read autonomy state: %w", err)
	}
	var decoded autonomyRegistry
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("parse autonomy state: %w", err)
	}
	if decoded.Items != nil {
		a.Items = decoded.Items
	}
	return nil
}

func (a *autonomyRegistry) saveLocked() error {
	tmp := a.path + ".tmp"
	payload, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, payload, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, a.path)
}

func (a *autonomyRegistry) show(sessionID string) (autonomyRecord, bool) {
	sessionID = strings.TrimSpace(sessionID)
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.Items[sessionID]
	return r, ok
}

func (a *autonomyRegistry) grant(sessionID, preset, scope string, until time.Time) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	rec := autonomyRecord{
		SessionID: sessionID,
		Preset:    strings.TrimSpace(preset),
		Scope:     strings.TrimSpace(scope),
		GrantedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if !until.IsZero() {
		rec.ExpiresAt = until.UTC().Format(time.RFC3339)
	}
	a.Items[sessionID] = rec
	return a.saveLocked()
}

func (a *autonomyRegistry) revoke(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.Items[sessionID]; !ok {
		return fmt.Errorf("no autonomy state for session %q", sessionID)
	}
	delete(a.Items, sessionID)
	return a.saveLocked()
}

func (a *autonomyRegistry) reset(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.Items, sessionID)
	return a.saveLocked()
}
