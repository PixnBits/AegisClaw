// Package permissions implements the capability-grant and visibility-policy model
// described in docs/specs/permissions-model.md.
package permissions

import (
	"errors"
	"time"
)

// ErrPermissionDenied is returned when a subject lacks a grant for a capability.
var ErrPermissionDenied = errors.New("ERR_PERMISSION_DENIED")

// Grant is an explicit allow record for a subject + capability pair.
type Grant struct {
	Subject    string `json:"subject"`
	Capability string `json:"capability"`
	GrantedBy  string `json:"granted_by"`
	GrantedAt  string `json:"granted_at"`
	Reason     string `json:"reason,omitempty"`
}

// VisibilityLevel controls whether a capability appears in discovery for a subject.
type VisibilityLevel string

const (
	VisibilityHidden       VisibilityLevel = "hidden"
	VisibilityGrantedOnly  VisibilityLevel = "granted_only"
	VisibilityRequestable  VisibilityLevel = "requestable"
	VisibilityPublic       VisibilityLevel = "public"
)

// VisibilityRule controls discovery exposure independent of grants.
type VisibilityRule struct {
	Subject    string          `json:"subject"`
	Capability string          `json:"capability"`
	Level      VisibilityLevel `json:"level"`
	Reason     string          `json:"reason,omitempty"`
	SetBy      string          `json:"set_by,omitempty"`
	SetAt      string          `json:"set_at,omitempty"`
}

// Request is emitted when a subject attempts to use an ungranted capability.
type Request struct {
	ID         string `json:"id"`
	Subject    string `json:"subject"`
	Capability string `json:"capability"`
	Context    string `json:"context,omitempty"`
	Timestamp  string `json:"timestamp"`
	Status     string `json:"status"` // pending, granted, denied
}

// Snapshot is the signed bundle pushed to microVMs after Hub handshake.
type Snapshot struct {
	Subject          string            `json:"subject"`
	AllowedTools     map[string]bool   `json:"allowed_tools"`
	VisibleTools     map[string]bool   `json:"visible_tools"`
	RequestableTools map[string]bool   `json:"requestable_tools"`
	CanDiscover      bool              `json:"can_discover_registry"`
	Version          int64             `json:"version"`
	Timestamp        string            `json:"timestamp"`
}

// State holds the durable permission + visibility state (Store source of truth).
type State struct {
	Grants     []Grant          `json:"grants"`
	Visibility []VisibilityRule `json:"visibility"`
	Requests   []Request        `json:"requests"`
	Version    int64            `json:"version"`
}

// NewState returns empty deny-by-default state.
func NewState() *State {
	return &State{
		Grants:     []Grant{},
		Visibility: []VisibilityRule{},
		Requests:   []Request{},
	}
}

// NowRFC3339 returns current UTC timestamp.
func NowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
