package storeapi

import (
	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"github.com/PixnBits/AegisClaw/internal/worker"
)

// AggregateStore is the main aggregator interface for all persistent state.
type AggregateStore interface {
	Proposals()  ProposalStore
	PullRequests() PullRequestStore
	Composition() CompositionStore
	Memory()     MemoryStore
	Workers()    WorkerStore
	Events()     EventStore
	Close() error
}

type ProposalStore interface {
	Create(p *proposal.Proposal) error
	Get(id string) (*proposal.Proposal, error)
	Update(p *proposal.Proposal) error
	List() ([]proposal.ProposalSummary, error)
	ListByStatus(status proposal.Status) ([]proposal.ProposalSummary, error)
	ResolveID(prefix string) (string, error)
	Import(p *proposal.Proposal) error
}

type PullRequestStore interface {
	Create(pr *pullrequest.PullRequest) error
	Get(id string) (*pullrequest.PullRequest, error)
	GetByProposalID(proposalID string) (*pullrequest.PullRequest, error)
	List(status *pullrequest.Status) ([]*pullrequest.PullRequest, error)
	Update(pr *pullrequest.PullRequest) error
	Approve(prID, approvedBy string) error
	Close(prID string) error
	MarkMerged(prID string) error
}

type CompositionStore interface {
	Publish(components map[string]composition.Component, actor, reason string) (*composition.Manifest, error)
	Current() *composition.Manifest
	Get(version int) (*composition.Manifest, error)
}

type MemoryStore interface {
	Store(entry *memory.MemoryEntry) (string, error)
	Retrieve(query string, k int, taskID string) ([]*memory.MemoryEntry, error)
	List(tier memory.TTLTier) ([]memory.StoreSummary, error)
}

type WorkerStore interface {
	Upsert(record *worker.WorkerRecord) error
	Get(id string) (*worker.WorkerRecord, bool)
	List(activeOnly bool) []*worker.WorkerRecord
}

type EventStore interface{}
