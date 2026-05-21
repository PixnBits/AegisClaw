package remote

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

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
	mu   sync.Mutex
}

// parseVsockAddr parses an address of the form "vsock://CID:PORT" and returns
// the CID and port as uint32 values.
func parseVsockAddr(addr string) (uint32, uint32, error) {
	trimmed := strings.TrimPrefix(addr, "vsock://")
	if trimmed == addr {
		return 0, 0, fmt.Errorf("vsock address must start with \"vsock://\", got: %q", addr)
	}
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("vsock address must be \"vsock://CID:PORT\", got: %q", addr)
	}
	cid64, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid CID %q in address %q: %w", parts[0], addr, err)
	}
	port64, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid port %q in address %q: %w", parts[1], addr, err)
	}
	return uint32(cid64), uint32(port64), nil
}

func NewRemoteClient(addr string) (*RemoteClient, error) {
	cid, port, err := parseVsockAddr(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid vsock address: %w", err)
	}
	conn, err := vsock.Dial(cid, port, nil)
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

// sendRequest is the core communication method.
// A mutex protects the shared connection so concurrent sub-store calls do not
// interleave their request/response frames on the stream.
func (c *RemoteClient) sendRequest(op string, payload interface{}) (interface{}, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := Request{
		ID:      op, // use op as a simple correlation key; callers are serialised by mu
		Op:      op,
		Payload: payload,
	}

	encoder := json.NewEncoder(c.conn)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	decoder := json.NewDecoder(c.conn)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("store vm error: %s", resp.Error)
	}

	return resp.Data, nil
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

// --- Proposal Store Implementation ---

type remoteProposalStore struct{ client *RemoteClient }

var _ store.ProposalStore = (*remoteProposalStore)(nil)

func (r *remoteProposalStore) Create(p *proposal.Proposal) error {
	_, err := r.client.sendRequest("proposal.create", p)
	return err
}

func (r *remoteProposalStore) Get(id string) (*proposal.Proposal, error) {
	data, err := r.client.sendRequest("proposal.get", map[string]string{"id": id})
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("proposal not found: %s", id)
	}
	var p proposal.Proposal
	if err := json.Unmarshal(data.(json.RawMessage), &p); err != nil {
		return nil, fmt.Errorf("unmarshal proposal get: %w", err)
	}
	return &p, nil
}

func (r *remoteProposalStore) Update(p *proposal.Proposal) error {
	_, err := r.client.sendRequest("proposal.update", p)
	return err
}

func (r *remoteProposalStore) List() ([]proposal.ProposalSummary, error) {
	data, err := r.client.sendRequest("proposal.list", nil)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var summaries []proposal.ProposalSummary
	if err := json.Unmarshal(data.(json.RawMessage), &summaries); err != nil {
		return nil, fmt.Errorf("unmarshal proposal list: %w", err)
	}
	return summaries, nil
}

func (r *remoteProposalStore) ListByStatus(status proposal.Status) ([]proposal.ProposalSummary, error) {
	data, err := r.client.sendRequest("proposal.list_by_status", map[string]string{"status": string(status)})
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var summaries []proposal.ProposalSummary
	if err := json.Unmarshal(data.(json.RawMessage), &summaries); err != nil {
		return nil, fmt.Errorf("unmarshal proposal list by status: %w", err)
	}
	return summaries, nil
}

func (r *remoteProposalStore) ResolveID(prefix string) (string, error) {
	data, err := r.client.sendRequest("proposal.resolve_id", map[string]string{"prefix": prefix})
	if err != nil {
		return "", err
	}
	if data == nil {
		return "", fmt.Errorf("no ID resolved for prefix: %s", prefix)
	}
	var resolved string
	if err := json.Unmarshal(data.(json.RawMessage), &resolved); err != nil {
		return "", fmt.Errorf("unmarshal proposal resolve id: %w", err)
	}
	return resolved, nil
}

func (r *remoteProposalStore) Import(p *proposal.Proposal) error {
	_, err := r.client.sendRequest("proposal.import", p)
	return err
}

// --- Other Store Stubs (Minimal implementations to satisfy interfaces) ---

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
