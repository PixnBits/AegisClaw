package store

import (
	"github.com/PixnBits/AegisClaw/internal/storeapi"
)

// Store is the main aggregator interface for all persistent state.
// Most components should depend on this rather than individual concrete stores.
type Store = storeapi.AggregateStore

// ProposalStore is a type alias for the shared interface.
type ProposalStore = storeapi.ProposalStore

// PullRequestStore is a type alias for the shared interface.
type PullRequestStore = storeapi.PullRequestStore

// CompositionStore is a type alias for the shared interface.
type CompositionStore = storeapi.CompositionStore

// MemoryStore is a type alias for the shared interface.
type MemoryStore = storeapi.MemoryStore

// WorkerStore is a type alias for the shared interface.
type WorkerStore = storeapi.WorkerStore

// EventStore is a type alias for the shared interface.
type EventStore = storeapi.EventStore
