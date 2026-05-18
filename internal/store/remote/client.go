package remote

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"github.com/PixnBits/AegisClaw/internal/store"
	"github.com/PixnBits/AegisClaw/internal/worker"
	"github.com/mdlayher/vsock"
)

// RemoteClient now actually talks to a Store VM over vsock.
type RemoteClient struct {
	conn net.Conn
}

func NewRemoteClient(addr string) (*RemoteClient, error) {
	// For now we expect addr like "vsock://3:9999"
	// In production this would parse CID and port
	conn, err := vsock.Dial(3, 9999, nil)
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

// Placeholder implementations for sub-stores. Remote mode is not enabled by
// default yet; these methods provide the correct typed surface so the package
// builds while the vsock protocol is completed.

type remoteProposalStore struct{ client *RemoteClient }

func (r *remoteProposalStore) Create(p *proposal.Proposal) error {
	_, err := r.client.sendRequest("proposal.create", p)
	return err
}

func (r *remoteProposalStore) Get(id string) (*proposal.Proposal, error) {
	return nil, fmt.Errorf("remote proposal get is not implemented for %s", id)
}

func (r *remoteProposalStore) Update(p *proposal.Proposal) error {
	_, err := r.client.sendRequest("proposal.update", p)
	return err
}

func (r *remoteProposalStore) List() ([]proposal.ProposalSummary, error) {
	return nil, fmt.Errorf("remote proposal list is not implemented")
}

func (r *remoteProposalStore) ListByStatus(status proposal.Status) ([]proposal.ProposalSummary, error) {
	return nil, fmt.Errorf("remote proposal list by status is not implemented for %s", status)
}

func (r *remoteProposalStore) ResolveID(prefix string) (string, error) {
	return "", fmt.Errorf("remote proposal resolve is not implemented for %s", prefix)
}

func (r *remoteProposalStore) Import(p *proposal.Proposal) error {
	_, err := r.client.sendRequest("proposal.import", p)
	return err
}

// Similar minimal implementations for other stores can be expanded

type remoteMemoryStore struct{ client *RemoteClient }

func (r *remoteMemoryStore) Store(entry *memory.MemoryEntry) (string, error) {
	_, err := r.client.sendRequest("memory.store", entry)
	return "", err
}

func (r *remoteMemoryStore) Retrieve(query string, k int, taskID string) ([]*memory.MemoryEntry, error) {
	return nil, fmt.Errorf("remote memory retrieve is not implemented for %q/%d/%q", query, k, taskID)
}

func (r *remoteMemoryStore) List(tier memory.TTLTier) ([]memory.StoreSummary, error) {
	return nil, fmt.Errorf("remote memory list is not implemented for tier %s", tier)
}

// Stubs for other stores

type remotePullRequestStore struct{ client *RemoteClient }

func (r *remotePullRequestStore) Create(pr *pullrequest.PullRequest) error { return nil }
func (r *remotePullRequestStore) Get(id string) (*pullrequest.PullRequest, error) {
	return nil, fmt.Errorf("remote pull request get is not implemented for %s", id)
}
func (r *remotePullRequestStore) GetByProposalID(proposalID string) (*pullrequest.PullRequest, error) {
	return nil, fmt.Errorf("remote pull request get by proposal is not implemented for %s", proposalID)
}
func (r *remotePullRequestStore) List(status *pullrequest.Status) ([]*pullrequest.PullRequest, error) {
	return nil, fmt.Errorf("remote pull request list is not implemented")
}
func (r *remotePullRequestStore) Update(pr *pullrequest.PullRequest) error { return nil }
func (r *remotePullRequestStore) Approve(prID, approvedBy string) error    { return nil }
func (r *remotePullRequestStore) Close(prID string) error                  { return nil }
func (r *remotePullRequestStore) MarkMerged(prID string) error             { return nil }

// ... (similar stubs for Composition, Worker, Event)

type remoteCompositionStore struct{ client *RemoteClient }

func (r *remoteCompositionStore) Publish(components map[string]composition.Component, actor, reason string) (*composition.Manifest, error) {
	return nil, fmt.Errorf("remote composition publish is not implemented")
}
func (r *remoteCompositionStore) Current() *composition.Manifest { return nil }
func (r *remoteCompositionStore) Get(version int) (*composition.Manifest, error) {
	return nil, fmt.Errorf("remote composition get is not implemented for v%d", version)
}

type remoteWorkerStore struct{ client *RemoteClient }

func (r *remoteWorkerStore) Upsert(record *worker.WorkerRecord) error    { return nil }
func (r *remoteWorkerStore) Get(id string) (*worker.WorkerRecord, bool)  { return nil, false }
func (r *remoteWorkerStore) List(activeOnly bool) []*worker.WorkerRecord { return nil }

type remoteEventStore struct{ client *RemoteClient }
