package remote

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/store"
)

// RemoteClient is a client that talks to a remote Store VM over vsock (or other transport).
// It implements the store.Store interface so it can be used interchangeably
// with the in-process implementation.
//
// Phase 2.8 skeleton: structure + basic method signatures.
// Actual vsock connection and request/response handling to be implemented in follow-up.
type RemoteClient struct {
	// conn vsock.Conn or similar in real implementation
	addr string // e.g. "vsock://2:1234" or future transport
}

// NewRemoteClient creates a client pointing at a Store VM.
func NewRemoteClient(addr string) (*RemoteClient, error) {
	// TODO (Phase 2.8+): Establish vsock connection here
	return &RemoteClient{addr: addr}, nil
}

// Ensure RemoteClient satisfies store.Store at compile time.
var _ store.Store = (*RemoteClient)(nil)

func (c *RemoteClient) Proposals() store.ProposalStore {
	return &remoteProposalStore{client: c}
}

func (c *RemoteClient) PullRequests() store.PullRequestStore {
	return &remotePullRequestStore{client: c}
}

func (c *RemoteClient) Composition() store.CompositionStore {
	// TODO: implement
	return nil
}

func (c *RemoteClient) Memory() store.MemoryStore {
	return &remoteMemoryStore{client: c}
}

func (c *RemoteClient) Workers() store.WorkerStore {
	// TODO: implement
	return nil
}

func (c *RemoteClient) Events() store.EventStore {
	// TODO: implement
	return nil
}

func (c *RemoteClient) Close() error {
	// TODO: close vsock connection
	return nil
}

// --- Sub-store implementations (stubs for now) ---

type remoteProposalStore struct {
	client *RemoteClient
}

func (r *remoteProposalStore) Create(ctx context.Context, p interface{}) error {
	// TODO: send Request{Op: OpProposalCreate, Payload: p}
	return fmt.Errorf("remote proposal create not yet implemented (Phase 2.8 skeleton)")
}

func (r *remoteProposalStore) Get(ctx context.Context, id string) (interface{}, error) {
	return nil, fmt.Errorf("remote proposal get not yet implemented")
}

func (r *remoteProposalStore) Update(ctx context.Context, p interface{}) error {
	return fmt.Errorf("remote proposal update not yet implemented")
}

func (r *remoteProposalStore) List(ctx context.Context, filter interface{}) ([]interface{}, error) {
	return nil, fmt.Errorf("remote proposal list not yet implemented")
}

func (r *remoteProposalStore) Import(p interface{}) error {
	return fmt.Errorf("remote proposal import not yet implemented")
}

// Similar stub implementations for MemoryStore, etc. can be added here.

type remoteMemoryStore struct {
	client *RemoteClient
}

func (r *remoteMemoryStore) Store(ctx context.Context, entry interface{}) error {
	return fmt.Errorf("remote memory store not yet implemented (Phase 2.8 skeleton)")
}

func (r *remoteMemoryStore) Search(ctx context.Context, query interface{}) ([]interface{}, error) {
	return nil, fmt.Errorf("remote memory search not yet implemented")
}

func (r *remoteMemoryStore) Get(ctx context.Context, id string) (interface{}, error) {
	return nil, fmt.Errorf("remote memory get not yet implemented")
}
