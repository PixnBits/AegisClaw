package eventbus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ApprovalStatus reflects the lifecycle of an approval request.
type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
	ApprovalExpired  ApprovalStatus = "expired"
)

// ApprovalRequest is a record requesting human sign-off for a high-risk operation.
type ApprovalRequest struct {
	// ApprovalID is a UUID assigned on creation.
	ApprovalID string `json:"approval_id"`
	// Title is a short human-readable description of what requires approval.
	Title string `json:"title"`
	// Description provides full context (what, why, risks).
	Description string `json:"description"`
	// RiskLevel is the declared risk: low, medium, high.
	RiskLevel string `json:"risk_level"`
	// Payload is arbitrary JSON context the agent supplies.
	Payload json.RawMessage `json:"payload,omitempty"`
	// TaskID links the request to an async task (optional).
	TaskID string `json:"task_id,omitempty"`
	// RequestedBy is the agent or component that raised the request.
	RequestedBy string `json:"requested_by"`
	// CreatedAt is when the request was made.
	CreatedAt time.Time `json:"created_at"`
	// ExpiresAt is the deadline for human response (nil = no expiry).
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// Status is the current lifecycle state.
	Status ApprovalStatus `json:"status"`
	// DecidedAt is when a human rendered the decision.
	DecidedAt *time.Time `json:"decided_at,omitempty"`
	// DecidedBy is the identity that approved or rejected.
	DecidedBy string `json:"decided_by,omitempty"`
	// Reason is the human-provided rationale for the decision.
	Reason string `json:"reason,omitempty"`
}

const approvalsFileName = "approvals.json"

// approvalStore is the persistent storage layer for approval requests.
type approvalStore struct {
	path string
	mu   sync.RWMutex
	data map[string]*ApprovalRequest
}

func newApprovalStore(dir string) (*approvalStore, error) {
	as := &approvalStore{
		path: filepath.Join(dir, approvalsFileName),
		data: make(map[string]*ApprovalRequest),
	}
	return as, as.load()
}

func (as *approvalStore) load() error {
	raw, err := os.ReadFile(as.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read approvals: %w", err)
	}
	var items []*ApprovalRequest
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("parse approvals: %w", err)
	}
	for _, a := range items {
		as.data[a.ApprovalID] = a
	}
	return nil
}

func (as *approvalStore) save() error {
	items := make([]*ApprovalRequest, 0, len(as.data))
	for _, a := range as.data {
		items = append(items, a)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal approvals: %w", err)
	}
	return atomicWriteFile(as.path, raw)
}

func (as *approvalStore) add(a *ApprovalRequest) error {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.data[a.ApprovalID] = a
	return as.save()
}

func (as *approvalStore) get(id string) (*ApprovalRequest, bool) {
	as.mu.RLock()
	defer as.mu.RUnlock()
	a, ok := as.data[id]
	if !ok {
		return nil, false
	}
	cp := *a
	return &cp, true
}

func (as *approvalStore) decide(id string, approved bool, decidedBy, reason string) error {
	as.mu.Lock()
	defer as.mu.Unlock()
	a, ok := as.data[id]
	if !ok {
		return fmt.Errorf("approval %s not found", id)
	}
	if a.Status != ApprovalPending {
		return fmt.Errorf("approval %s is not pending (status: %s)", id, a.Status)
	}
	now := time.Now().UTC()
	a.DecidedAt = &now
	a.DecidedBy = decidedBy
	a.Reason = reason
	if approved {
		a.Status = ApprovalApproved
	} else {
		a.Status = ApprovalRejected
	}
	return as.save()
}

// listPending returns all pending approval requests, sorted by creation time.
func (as *approvalStore) listPending() []*ApprovalRequest {
	as.mu.RLock()
	defer as.mu.RUnlock()
	var out []*ApprovalRequest
	for _, a := range as.data {
		if a.Status == ApprovalPending {
			cp := *a
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// list returns all approval requests regardless of status, newest first.
func (as *approvalStore) list() []*ApprovalRequest {
	as.mu.RLock()
	defer as.mu.RUnlock()
	var out []*ApprovalRequest
	for _, a := range as.data {
		cp := *a
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// countPending returns the number of pending approval requests.
func (as *approvalStore) countPending() int {
	as.mu.RLock()
	defer as.mu.RUnlock()
	n := 0
	for _, a := range as.data {
		if a.Status == ApprovalPending {
			n++
		}
	}
	return n
}

// newApprovalID generates a new UUID for an approval request.
func newApprovalID() string { return uuid.New().String() }
