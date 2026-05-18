package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"github.com/PixnBits/AegisClaw/internal/store"
	"github.com/mdlayher/vsock"
)

// RemoteClient now actually talks to a Store VM over vsock.
type RemoteClient struct {
	conn net.Conn
}

func NewRemoteClient(addr string) (*RemoteClient, error) {
	// For now we expect addr like "vsock://3:9999"
	// In production this would parse CID and port
	conn, err := vsock.Dial(3, 9999)
	if err != nil {
		return nil, fmt.Errorf("vsock dial to Store VM failed: %w", err)
	}
	return &RemoteClient{conn: conn}, nil
}

func (c *RemoteClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// sendRequest is the core communication method
func (c *RemoteClient) sendRequest(op string, payload interface{}) (interface{}, error) {
	req := map[string]interface{}{
		"op":      op,
		"payload": payload,
	}

	encoder := json.NewEncoder(c.conn)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	decoder := json.NewDecoder(c.conn)
	var resp map[string]interface{}
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if errStr, ok := resp["error"].(string); ok && errStr != "" {
		return nil, fmt.Errorf("store vm error: %s", errStr)
	}

	return resp, nil
}

// --- Store interface implementations ---

func (c *RemoteClient) Proposals() store.ProposalStore {
	return &remoteProposalStore{client: c}
}

func (c *RemoteClient) PullRequests() store.PullRequestStore {
	return &remotePullRequestStore{client: c}
}

func (c *RemoteClient) Composition() store.CompositionStore {
	return &remoteCompositionStore{client: c}
}

func (c *RemoteClient) Memory() store.MemoryStore {
	return &remoteMemoryStore{client: c}
}

func (c *RemoteClient) Workers() store.WorkerStore {
	return &remoteWorkerStore{client: c}
}

func (c *RemoteClient) Events() store.EventStore {
	return &remoteEventStore{client: c}
}

// Placeholder implementations for sub-stores

type remoteProposalStore struct{ client *RemoteClient }

func (r *remoteProposalStore) Create(ctx context.Context, p interface{}) error {
	_, err := r.client.sendRequest("proposal.create", p)
	return err
}

func (r *remoteProposalStore) Get(ctx context.Context, id string) (interface{}, error) {
	return r.client.sendRequest("proposal.get", map[string]string{"id": id})
}

func (r *remoteProposalStore) Update(ctx context.Context, p interface{}) error {
	_, err := r.client.sendRequest("proposal.update", p)
	return err
}

func (r *remoteProposalStore) List(ctx context.Context, filter interface{}) ([]interface{}, error) {
	res, err := r.client.sendRequest("proposal.list", filter)
	if err != nil {
		return nil, err
	}
	// TODO: properly convert
	return nil, nil
}

func (r *remoteProposalStore) Import(p interface{}) error {
	_, err := r.client.sendRequest("proposal.import", p)
	return err
}

// Similar minimal implementations for other stores can be expanded

type remoteMemoryStore struct{ client *RemoteClient }

func (r *remoteMemoryStore) Store(ctx context.Context, entry interface{}) error {
	_, err := r.client.sendRequest("memory.store", entry)
	return err
}

func (r *remoteMemoryStore) Search(ctx context.Context, query interface{}) ([]interface{}, error) {
	_, err := r.client.sendRequest("memory.search", query)
	return nil, err
}

func (r *remoteMemoryStore) Get(ctx context.Context, id string) (interface{}, error) {
	return r.client.sendRequest("memory.get", map[string]string{"id": id})
}

// Stubs for other stores

type remotePullRequestStore struct{ client *RemoteClient }
func (r *remotePullRequestStore) Create(ctx context.Context, pr interface{}) error { return nil }
func (r *remotePullRequestStore) Get(ctx context.Context, id string) (interface{}, error) { return nil, nil }
func (r *remotePullRequestStore) List(ctx context.Context, f interface{}) ([]interface{}, error) { return nil, nil }
func (r *remotePullRequestStore) Update(ctx context.Context, pr interface{}) error { return nil }

// ... (similar stubs for Composition, Worker, Event)

type remoteCompositionStore struct{ client *RemoteClient }
func (r *remoteCompositionStore) Publish(components map[string]interface{}, actor, reason string) error { return nil }
func (r *remoteCompositionStore) GetLatest(ctx context.Context) (interface{}, error) { return nil, nil }

// Add more as needed
