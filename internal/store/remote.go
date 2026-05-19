package store

import (
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"github.com/PixnBits/AegisClaw/internal/worker"
)

// remoteStore is the production seam for Store VM access.
// All methods currently return ErrRemoteNotWired until vsock + AegisHub
// routing is implemented.
//
// This file will be replaced by a real client; no local fallback remains
// in the daemon. The Host Daemon never creates or owns persistent stores.
type remoteStore struct{}

func NewRemoteStore() Store {
	return &remoteStore{}
}

func (r *remoteStore) Proposals() ProposalStore       { return &remoteProposalStore{} }
func (r *remoteStore) PullRequests() PullRequestStore { return &remotePullRequestStore{} }
func (r *remoteStore) Composition() CompositionStore  { return &remoteCompositionStore{} }
func (r *remoteStore) Memory() MemoryStore            { return &remoteMemoryStore{} }
func (r *remoteStore) Workers() WorkerStore           { return &remoteWorkerStore{} }
func (r *remoteStore) Events() EventStore             { return &remoteEventStore{} }
func (r *remoteStore) Close() error                   { return nil }

// Compile-time checks
var _ Store = (*remoteStore)(nil)
var _ ProposalStore       = (*remoteProposalStore)(nil)
var _ PullRequestStore    = (*remotePullRequestStore)(nil)
var _ CompositionStore    = (*remoteCompositionStore)(nil)
var _ MemoryStore         = (*remoteMemoryStore)(nil)
var _ WorkerStore         = (*remoteWorkerStore)(nil)
var _ EventStore          = (*remoteEventStore)(nil)

// The sub-type stubs below will be replaced with real vsock implementations
// once the Store VM image and protocol are ready.

type remoteProposalStore struct{}

func (r *remoteProposalStore) Create(p *proposal.Proposal) error { return ErrRemoteNotWired }
func (r *remoteProposalStore) Get(id string) (*proposal.Proposal, error) {
	return nil, ErrRemoteNotWired
}
func (r *remoteProposalStore) Update(p *proposal.Proposal) error { return ErrRemoteNotWired }
func (r *remoteProposalStore) List() ([]proposal.ProposalSummary, error) {
	return nil, ErrRemoteNotWired
}
func (r *remoteProposalStore) ListByStatus(status proposal.Status) ([]proposal.ProposalSummary, error) {
	return nil, ErrRemoteNotWired
}
func (r *remoteProposalStore) ResolveID(prefix string) (string, error) {
	return "", ErrRemoteNotWired
}
func (r *remoteProposalStore) Import(p *proposal.Proposal) error { return ErrRemoteNotWired }

type remotePullRequestStore struct{}

func (r *remotePullRequestStore) Create(pr *pullrequest.PullRequest) error {
	return ErrRemoteNotWired
}
func (r *remotePullRequestStore) Get(id string) (*pullrequest.PullRequest, error) {
	return nil, ErrRemoteNotWired
}
func (r *remotePullRequestStore) GetByProposalID(proposalID string) (*pullrequest.PullRequest, error) {
	return nil, ErrRemoteNotWired
}
func (r *remotePullRequestStore) List(status *pullrequest.Status) ([]*pullrequest.PullRequest, error) {
	return nil, ErrRemoteNotWired
}
func (r *remotePullRequestStore) Update(pr *pullrequest.PullRequest) error {
	return ErrRemoteNotWired
}
func (r *remotePullRequestStore) Approve(prID, approvedBy string) error {
	return ErrRemoteNotWired
}
func (r *remotePullRequestStore) Close(prID string) error  { return ErrRemoteNotWired }
func (r *remotePullRequestStore) MarkMerged(prID string) error { return ErrRemoteNotWired }

type remoteCompositionStore struct{}

func (r *remoteCompositionStore) Publish(components map[string]composition.Component, actor, reason string) (*composition.Manifest, error) {
	return nil, ErrRemoteNotWired
}
func (r *remoteCompositionStore) Current() *composition.Manifest { return nil }
func (r *remoteCompositionStore) Get(version int) (*composition.Manifest, error) {
	return nil, ErrRemoteNotWired
}

type remoteMemoryStore struct{}

func (r *remoteMemoryStore) Store(entry *memory.MemoryEntry) (string, error) {
	return "", ErrRemoteNotWired
}
func (r *remoteMemoryStore) Retrieve(query string, k int, taskID string) ([]*memory.MemoryEntry, error) {
	return nil, ErrRemoteNotWired
}
func (r *remoteMemoryStore) List(tier memory.TTLTier) ([]memory.StoreSummary, error) {
	return nil, ErrRemoteNotWired
}

type remoteWorkerStore struct{}

func (r *remoteWorkerStore) Upsert(record *worker.WorkerRecord) error {
	return ErrRemoteNotWired
}
func (r *remoteWorkerStore) Get(id string) (*worker.WorkerRecord, bool) { return nil, false }
func (r *remoteWorkerStore) List(activeOnly bool) []*worker.WorkerRecord { return nil }

type remoteEventStore struct{}

// ErrRemoteNotWired is returned by all remote store methods until the
// real vsock + AegisHub routing to Store VM is implemented (Phase 4).
var ErrRemoteNotWired = fmt.Errorf("remote Store VM seam not wired (vsock + AegisHub pending; see Phase 4)")
