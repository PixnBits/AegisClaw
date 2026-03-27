package kernel

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ActionType categorizes kernel operations for routing and auditing.
type ActionType string

const (
	ActionKernelStart     ActionType = "kernel.start"
	ActionKernelStop      ActionType = "kernel.stop"
	ActionSandboxCreate   ActionType = "sandbox.create"
	ActionSandboxStart    ActionType = "sandbox.start"
	ActionSandboxStop     ActionType = "sandbox.stop"
	ActionSandboxDelete   ActionType = "sandbox.delete"
	ActionSkillRegister   ActionType = "skill.register"
	ActionSkillActivate   ActionType = "skill.activate"
	ActionSkillDeactivate ActionType = "skill.deactivate"
	ActionSkillInvoke     ActionType = "skill.invoke"
	ActionMessageRoute    ActionType = "message.route"
	ActionControlPlane    ActionType = "controlplane.message"
	ActionProposalCreate  ActionType = "proposal.create"
	ActionProposalSubmit  ActionType = "proposal.submit"
	ActionProposalReview  ActionType = "proposal.review"
	ActionProposalApprove ActionType = "proposal.approve"
	ActionProposalReject  ActionType = "proposal.reject"
	ActionProposalVote    ActionType = "proposal.vote"
	ActionBuilderCreate   ActionType = "builder.create"
	ActionBuilderStart    ActionType = "builder.start"
	ActionBuilderStop     ActionType = "builder.stop"
	ActionBuilderBuild    ActionType = "builder.build"
	ActionSecretAdd       ActionType = "secret.add"
	ActionSecretGet       ActionType = "secret.get"
	ActionSecretDelete    ActionType = "secret.delete"
)

// validActionTypes enumerates all recognized action types for validation.
var validActionTypes = map[ActionType]bool{
	ActionKernelStart:     true,
	ActionKernelStop:      true,
	ActionSandboxCreate:   true,
	ActionSandboxStart:    true,
	ActionSandboxStop:     true,
	ActionSandboxDelete:   true,
	ActionSkillRegister:   true,
	ActionSkillActivate:   true,
	ActionSkillDeactivate: true,
	ActionSkillInvoke:     true,
	ActionMessageRoute:    true,
	ActionControlPlane:    true,
	ActionProposalCreate:  true,
	ActionProposalSubmit:  true,
	ActionProposalReview:  true,
	ActionProposalApprove: true,
	ActionProposalReject:  true,
	ActionProposalVote:    true,
	ActionBuilderCreate:   true,
	ActionBuilderStart:    true,
	ActionBuilderStop:     true,
	ActionBuilderBuild:    true,
	ActionSecretAdd:       true,
	ActionSecretGet:       true,
	ActionSecretDelete:    true,
}

// Action represents any operation that passes through the kernel.
// Every action is signed and logged for tamper-evident auditing.
type Action struct {
	ID        string     `json:"id"`
	Type      ActionType `json:"type"`
	Source    string     `json:"source"`
	Timestamp time.Time  `json:"timestamp"`
	Payload   []byte     `json:"payload,omitempty"`
}

// SignedAction wraps an Action with its Ed25519 cryptographic signature.
type SignedAction struct {
	Action    Action `json:"action"`
	Signature []byte `json:"signature"`
}

// NewAction creates a new Action with a generated UUID and current UTC timestamp.
func NewAction(actionType ActionType, source string, payload []byte) Action {
	return Action{
		ID:        uuid.New().String(),
		Type:      actionType,
		Source:    source,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

// Validate checks that the action has all required fields and a recognized type.
func (a *Action) Validate() error {
	if a.ID == "" {
		return fmt.Errorf("action ID is required")
	}
	if _, err := uuid.Parse(a.ID); err != nil {
		return fmt.Errorf("action ID is not a valid UUID: %w", err)
	}
	if a.Type == "" {
		return fmt.Errorf("action type is required")
	}
	if !validActionTypes[a.Type] {
		return fmt.Errorf("unrecognized action type: %s", a.Type)
	}
	if a.Source == "" {
		return fmt.Errorf("action source is required")
	}
	if a.Timestamp.IsZero() {
		return fmt.Errorf("action timestamp is required")
	}
	return nil
}

// Marshal serializes the Action to deterministic JSON for signing.
func (a *Action) Marshal() ([]byte, error) {
	return json.Marshal(a)
}
