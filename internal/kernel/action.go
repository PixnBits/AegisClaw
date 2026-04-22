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
	ActionKernelStart         ActionType = "kernel.start"
	ActionKernelStop          ActionType = "kernel.stop"
	ActionSandboxCreate       ActionType = "sandbox.create"
	ActionSandboxStart        ActionType = "sandbox.start"
	ActionSandboxStop         ActionType = "sandbox.stop"
	ActionSandboxDelete       ActionType = "sandbox.delete"
	ActionSkillRegister       ActionType = "skill.register"
	ActionSkillActivate       ActionType = "skill.activate"
	ActionSkillDeactivate     ActionType = "skill.deactivate"
	ActionSkillInvoke         ActionType = "skill.invoke"
	ActionMessageRoute        ActionType = "message.route"
	ActionControlPlane        ActionType = "controlplane.message"
	ActionProposalCreate      ActionType = "proposal.create"
	ActionProposalSubmit      ActionType = "proposal.submit"
	ActionProposalReview      ActionType = "proposal.review"
	ActionProposalApprove     ActionType = "proposal.approve"
	ActionProposalReject      ActionType = "proposal.reject"
	ActionProposalEscalate    ActionType = "proposal.escalate"
	ActionProposalVote        ActionType = "proposal.vote"
	ActionBuilderCreate       ActionType = "builder.create"
	ActionBuilderStart        ActionType = "builder.start"
	ActionBuilderStop         ActionType = "builder.stop"
	ActionBuilderBuild        ActionType = "builder.build"
	ActionSecretAdd           ActionType = "secret.add"
	ActionSecretGet           ActionType = "secret.get"
	ActionSecretDelete        ActionType = "secret.delete"
	ActionCompositionRollback ActionType = "composition.rollback"
	ActionLLMInfer            ActionType = "llm.infer"
	// ActionSnapshotCreate is logged when a VM snapshot is created (memory + disk state).
	ActionSnapshotCreate ActionType = "snapshot.create"
	// ActionSnapshotRestore is logged when a VM is restored from a snapshot.
	ActionSnapshotRestore ActionType = "snapshot.restore"
	// ActionMemoryStore is logged when an agent writes an entry to the Memory Store.
	ActionMemoryStore ActionType = "memory.store"
	// ActionMemoryRetrieve is logged when an agent queries the Memory Store.
	ActionMemoryRetrieve ActionType = "memory.retrieve"
	// ActionMemoryDelete is logged when entries are soft-deleted from the Memory Store.
	ActionMemoryDelete ActionType = "memory.delete"
	// ActionMemoryCompact is logged when the Memory Store compaction daemon runs.
	ActionMemoryCompact ActionType = "memory.compact"
	// ActionEventTimerSet is logged when an agent creates a timer.
	ActionEventTimerSet ActionType = "event.timer.set"
	// ActionEventTimerCancel is logged when a timer is cancelled.
	ActionEventTimerCancel ActionType = "event.timer.cancel"
	// ActionEventTimerFired is logged by the timer daemon when a timer fires.
	ActionEventTimerFired ActionType = "event.timer.fired"
	// ActionEventSubscribe is logged when an agent registers a signal subscription.
	ActionEventSubscribe ActionType = "event.subscribe"
	// ActionEventUnsubscribe is logged when an agent removes a signal subscription.
	ActionEventUnsubscribe ActionType = "event.unsubscribe"
	// ActionApprovalRequest is logged when an agent requests human approval.
	ActionApprovalRequest ActionType = "approval.request"
	// ActionApprovalDecide is logged when a human approves or rejects a request.
	ActionApprovalDecide ActionType = "approval.decide"
	// ActionWorkerSpawn is logged when the Orchestrator spawns an ephemeral Worker.
	ActionWorkerSpawn ActionType = "worker.spawn"
	// ActionWorkerComplete is logged when a Worker finishes successfully.
	ActionWorkerComplete ActionType = "worker.complete"
	// ActionWorkerTimeout is logged when a Worker exceeds its deadline.
	ActionWorkerTimeout ActionType = "worker.timeout"
	// ActionWorkerDestroy is logged when a Worker VM is stopped and deleted.
	ActionWorkerDestroy ActionType = "worker.destroy"
	// ActionSystemComponentActivate is logged when a core system microVM
	// (e.g. AegisHub) is launched at daemon startup. These are distinct from
	// skill activations which go through user-initiated proposals.
	ActionSystemComponentActivate ActionType = "system.component.activate"
	// ActionKBIngest is logged when a document is ingested into the Knowledge Base raw store.
	ActionKBIngest ActionType = "kb.ingest"
	// ActionKBCompile is logged when the KB Compiler runs and updates wiki pages.
	ActionKBCompile ActionType = "kb.compile"
	// ActionKBLint is logged when the KB Linter scans the wiki for issues.
	ActionKBLint ActionType = "kb.lint"
	// ActionKBQuery is logged when the Knowledge Base is queried.
	ActionKBQuery ActionType = "kb.query"
)

// validActionTypes enumerates all recognized action types for validation.
var validActionTypes = map[ActionType]bool{
	ActionKernelStart:         true,
	ActionKernelStop:          true,
	ActionSandboxCreate:       true,
	ActionSandboxStart:        true,
	ActionSandboxStop:         true,
	ActionSandboxDelete:       true,
	ActionSkillRegister:       true,
	ActionSkillActivate:       true,
	ActionSkillDeactivate:     true,
	ActionSkillInvoke:         true,
	ActionMessageRoute:        true,
	ActionControlPlane:        true,
	ActionProposalCreate:      true,
	ActionProposalSubmit:      true,
	ActionProposalReview:      true,
	ActionProposalApprove:     true,
	ActionProposalReject:      true,
	ActionProposalEscalate:    true,
	ActionProposalVote:        true,
	ActionBuilderCreate:       true,
	ActionBuilderStart:        true,
	ActionBuilderStop:         true,
	ActionBuilderBuild:        true,
	ActionSecretAdd:           true,
	ActionSecretGet:           true,
	ActionSecretDelete:        true,
	ActionCompositionRollback:     true,
	ActionLLMInfer:                true,
	ActionSnapshotCreate:          true,
	ActionSnapshotRestore:         true,
	ActionMemoryStore:             true,
	ActionMemoryRetrieve:          true,
	ActionMemoryDelete:            true,
	ActionMemoryCompact:           true,
	ActionEventTimerSet:           true,
	ActionEventTimerCancel:        true,
	ActionEventTimerFired:         true,
	ActionEventSubscribe:          true,
	ActionEventUnsubscribe:        true,
	ActionApprovalRequest:         true,
	ActionApprovalDecide:          true,
	ActionWorkerSpawn:             true,
	ActionWorkerComplete:          true,
	ActionWorkerTimeout:           true,
	ActionWorkerDestroy:           true,
	ActionSystemComponentActivate: true,
	ActionKBIngest:                 true,
	ActionKBCompile:                true,
	ActionKBLint:                   true,
	ActionKBQuery:                  true,
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
