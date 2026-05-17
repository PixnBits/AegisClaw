package store

import (
	"context"

	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"github.com/PixnBits/AegisClaw/internal/worker"
)

// localStore is an in-process implementation of Store.
// It holds references to the concrete store implementations.
// This is the initial implementation used while we migrate persistent state
// ownership out of the Host Daemon and toward a future Store VM.
type localStore struct {
	proposals     ProposalStore
	pullRequests  PullRequestStore
	composition   CompositionStore
	memory        MemoryStore
	workers       WorkerStore
	events        EventStore
}

// NewLocal creates a new localStore.
// The individual stores should be created and passed in from the caller
// (currently from initRuntime in cmd/aegisclaw).
func NewLocal(
	proposals ProposalStore,
	pullRequests PullRequestStore,
	composition CompositionStore,
	memory MemoryStore,
	workers WorkerStore,
	events EventStore,
) Store {
	return &localStore{
		proposals:    proposals,
		pullRequests: pullRequests,
		composition:  composition,
		memory:       memory,
		workers:      workers,
		events:       events,
	}
}

func (s *localStore) Proposals() ProposalStore     { return s.proposals }
func (s *localStore) PullRequests() PullRequestStore { return s.pullRequests }
func (s *localStore) Composition() CompositionStore { return s.composition }
func (s *localStore) Memory() MemoryStore           { return s.memory }
func (s *localStore) Workers() WorkerStore         { return s.workers }
func (s *localStore) Events() EventStore           { return s.events }

func (s *localStore) Close() error {
	// For now, individual stores manage their own lifecycle.
	// In the future, this can coordinate closing remote connections, etc.
	return nil
}

// Compile-time check that localStore implements Store.
var _ Store = (*localStore)(nil)
