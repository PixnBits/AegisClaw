package store

import (
	"context"

	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"github.com/PixnBits/AegisClaw/internal/worker"
)

// Store is the main aggregator interface for all persistent state.
// Most components should depend on this rather than individual concrete stores.
type Store interface {
	Proposals() ProposalStore
	PullRequests() PullRequestStore
	Composition() CompositionStore
	Memory() MemoryStore
	Workers() WorkerStore
	Events() EventStore

	// Close releases any resources held by the underlying stores.
	Close() error
}

// ProposalStore manages skill and governance proposals.
type ProposalStore interface {
	Create(ctx context.Context, p *proposal.Proposal) error
	Get(ctx context.Context, id string) (*proposal.Proposal, error)
	Update(ctx context.Context, p *proposal.Proposal) error
	List(ctx context.Context, filter proposal.Filter) ([]*proposal.Proposal, error)
	Import(p *proposal.Proposal) error // used when importing from CLI
}

// PullRequestStore manages pull request metadata.
type PullRequestStore interface {
	Create(ctx context.Context, pr *pullrequest.PullRequest) error
	Get(ctx context.Context, id string) (*pullrequest.PullRequest, error)
	List(ctx context.Context, filter pullrequest.Filter) ([]*pullrequest.PullRequest, error)
	Update(ctx context.Context, pr *pullrequest.PullRequest) error
}

// CompositionStore manages published composition manifests.
type CompositionStore interface {
	Publish(components map[string]composition.Component, actor, reason string) error
	GetLatest(ctx context.Context) (*composition.Manifest, error)
}

// MemoryStore manages per-agent long-term and short-term memory.
type MemoryStore interface {
	Store(ctx context.Context, entry *memory.Entry) error
	Search(ctx context.Context, query memory.Query) ([]*memory.Entry, error)
	Get(ctx context.Context, id string) (*memory.Entry, error)
}

// WorkerStore manages worker lifecycle records.
type WorkerStore interface {
	Create(ctx context.Context, record *worker.Record) error
	Get(ctx context.Context, id string) (*worker.Record, error)
	ListActive(ctx context.Context) ([]*worker.Record, error)
	Update(ctx context.Context, record *worker.Record) error
}

// EventStore is a placeholder for persistent event/timer/subscription storage.
// The full interface will be defined as EventBus persistence needs are clarified.
type EventStore interface {
	// TODO: Define timer, subscription, and approval queue methods
}
