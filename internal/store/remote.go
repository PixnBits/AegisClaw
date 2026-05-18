package store

import (
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"github.com/PixnBits/AegisClaw/internal/worker"
)

// remoteStore is a placeholder client that will eventually talk to the
// real Store VM over vsock routed through AegisHub.
//
// During Phase 2 this is just a seam. All methods return "not implemented"
// errors so the daemon can compile and run with the local fallback.
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

// Compile-time check
var _ Store = (*remoteStore)(nil)

// The sub-type stubs below will be replaced with real vsock implementations
// once the Store VM image and protocol are ready.

type remoteProposalStore struct{}

func (r *remoteProposalStore) Create(p *proposal.Proposal) error { return errRemoteNotWired }
func (r *remoteProposalStore) Get(id string) (*proposal.Proposal, error) {
	return nil, errRemoteNotWired
}
func (r *remoteProposalStore) Update(p *proposal.Proposal) error { return errRemoteNotWired }
func (r *remoteProposalStore) List() ([]proposal.ProposalSummary, error) {
	return nil, errRemoteNotWired
}
func (r *remoteProposalStore) ListByStatus(status proposal.Status) ([]proposal.ProposalSummary, error) {
	return nil, errRemoteNotWired
}
func (r *remoteProposalStore) ResolveID(prefix string) (string, error) {
	return "", errRemoteNotWired
}
func (r *remoteProposalStore) Import(p *proposal.Proposal) error { return errRemoteNotWired }

type remotePullRequestStore struct{}

func (r *remotePullRequestStore) Create(pr *pullrequest.PullRequest) error {
	return errRemoteNotWired
}
func (r *remotePullRequestStore) Get(id string) (*pullrequest.PullRequest, error) {
	return nil, errRemoteNotWired
}
func (r *remotePullRequestStore) GetByProposalID(proposalID string) (*pullrequest.PullRequest, error) {
	return nil, errRemoteNotWired
}
func (r *remotePullRequestStore) List(status *pullrequest.Status) ([]*pullrequest.PullRequest, error) {
	return nil, errRemoteNotWired
}
func (r *remotePullRequestStore) Update(pr *pullrequest.PullRequest) error {
	return errRemoteNotWired
}
func (r *remotePullRequestStore) Approve(prID, approvedBy string) error {
	return errRemoteNotWired
}
func (r *remotePullRequestStore) Close(prID string) error  { return errRemoteNotWired }
func (r *remotePullRequestStore) MarkMerged(prID string) error { return errRemoteNotWired }

type remoteCompositionStore struct{}

func (r *remoteCompositionStore) Publish(components map[string]composition.Component, actor, reason string) (*composition.Manifest, error) {
	return nil, errRemoteNotWired
}
func (r *remoteCompositionStore) Current() *composition.Manifest { return nil }
func (r *remoteCompositionStore) Get(version int) (*composition.Manifest, error) {
	return nil, errRemoteNotWired
}

type remoteMemoryStore struct{}

func (r *remoteMemoryStore) Store(entry *memory.MemoryEntry) (string, error) {
	return "", errRemoteNotWired
}
func (r *remoteMemoryStore) Retrieve(query string, k int, taskID string) ([]*memory.MemoryEntry, error) {
	return nil, errRemoteNotWired
}
func (r *remoteMemoryStore) List(tier memory.TTLTier) ([]memory.StoreSummary, error) {
	return nil, errRemoteNotWired
}

type remoteWorkerStore struct{}

func (r *remoteWorkerStore) Upsert(record *worker.WorkerRecord) error {
	return errRemoteNotWired
}
func (r *remoteWorkerStore) Get(id string) (*worker.WorkerRecord, bool) { return nil, false }
func (r *remoteWorkerStore) List(activeOnly bool) []*worker.WorkerRecord { return nil }

type remoteEventStore struct{}

var errRemoteNotWired = fmt.Errorf("remote Store VM not yet wired (TODO: vsock + AegisHub routing)")
