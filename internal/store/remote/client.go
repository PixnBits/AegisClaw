package remote

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"github.com/PixnBits/AegisClaw/internal/storeapi"
	"github.com/PixnBits/AegisClaw/internal/worker"
	"github.com/mdlayher/vsock"
)

// Security Constants
const (
	// MaxPayloadLen limits request/response payloads to 4 MiB to prevent DoS.
	MaxPayloadLen = 4 * 1024 * 1024
	// handshakeTimeout limits the time allowed for the initial authentication handshake.
	handshakeTimeout = 5 * time.Second
)

// Request is sent from client (AegisHub/daemon) to Store VM.
type Request struct {
	ID      string          `json:"id"`
	Op      string          `json:"op"` // e.g. "proposal.create", "memory.store", "list_proposals"
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Response is returned by the Store VM.
type Response struct {
	ID      string          `json:"id"`
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// RemoteClient now actually talks to a Store VM over vsock.
type RemoteClient struct {
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex
}

var _ storeapi.AggregateStore = (*RemoteClient)(nil)

type ProposalStoreImpl struct{ client *RemoteClient }

type PullRequestStoreImpl struct{ client *RemoteClient }

type CompositionStoreImpl struct{ client *RemoteClient }

type MemoryStoreImpl struct{ client *RemoteClient }

type WorkerStoreImpl struct{ client *RemoteClient }

type EventStoreImpl struct{ client *RemoteClient }

var _ storeapi.ProposalStore = (*ProposalStoreImpl)(nil)
var _ storeapi.PullRequestStore = (*PullRequestStoreImpl)(nil)
var _ storeapi.CompositionStore = (*CompositionStoreImpl)(nil)
var _ storeapi.MemoryStore = (*MemoryStoreImpl)(nil)
var _ storeapi.WorkerStore = (*WorkerStoreImpl)(nil)

// --- AggregateStore interface methods on RemoteClient ---

func (c *RemoteClient) Proposals() storeapi.ProposalStore {
	return &ProposalStoreImpl{client: c}
}

func (c *RemoteClient) PullRequests() storeapi.PullRequestStore {
	return &PullRequestStoreImpl{client: c}
}

func (c *RemoteClient) Composition() storeapi.CompositionStore {
	return &CompositionStoreImpl{client: c}
}

func (c *RemoteClient) Memory() storeapi.MemoryStore {
	return &MemoryStoreImpl{client: c}
}

func (c *RemoteClient) Workers() storeapi.WorkerStore {
	return &WorkerStoreImpl{client: c}
}

func (c *RemoteClient) Events() storeapi.EventStore {
	return &EventStoreImpl{client: c}
}

func (c *RemoteClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// --- vsock helper ---

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

	// Task 1: Mutual Authentication Handshake
	// Trust Boundary: The vsock connection is untrusted until authenticated.
	// All external input is considered hostile until proven otherwise.
	secret := os.Getenv("STORE_VM_SHARED_SECRET")
	if secret == "" {
		conn.Close()
		return nil, fmt.Errorf("STORE_VM_SHARED_SECRET environment variable is required for client authentication")
	}

	if err := performHandshake(conn, secret); err != nil {
		conn.Close()
		return nil, fmt.Errorf("vsock handshake failed: %w", err)
	}

	return &RemoteClient{
		conn:   conn,
		reader: bufio.NewReader(conn),
	}, nil
}

// performHandshake sends the authentication token and waits for confirmation.
func performHandshake(conn net.Conn, secret string) error {
	conn.SetReadDeadline(time.Now().Add(handshakeTimeout))
	defer conn.SetReadDeadline(time.Time{})

	handshakeReq := map[string]string{
		"type":   "handshake",
		"secret": secret,
	}
	if err := json.NewEncoder(conn).Encode(handshakeReq); err != nil {
		return fmt.Errorf("failed to send handshake: %w", err)
	}

	var resp map[string]string
	if err := newLimitedDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("failed to read handshake response: %w", err)
	}

	if resp["type"] != "handshake_ack" || resp["status"] != "ok" {
		return fmt.Errorf("invalid handshake response: %v", resp)
	}
	return nil
}

// SanitizeError returns a generic error message for external consumption while
// preserving the original error for internal logging. This prevents information leakage.
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	return "internal error"
}

// --- Core request method ---

// sendRequest is the core communication method.
// A mutex protects the shared connection so concurrent sub-store calls do not
// interleave their request/response frames on the stream.
// Returns json.RawMessage to avoid double-unmarshaling issues when the server
// sends Go types that json.Decoder converts to map/slice.
func (c *RemoteClient) sendRequest(op string, payload interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Enforce payload size limit to prevent DoS.
	var payloadBytes []byte
	if payload != nil {
		var err error
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		if len(payloadBytes) > MaxPayloadLen {
			return nil, fmt.Errorf("payload exceeds maximum allowed size of %d bytes", MaxPayloadLen)
		}
	}

	req := Request{
		ID:      op, // use op as a simple correlation key; callers are serialised by mu
		Op:      op,
		Payload: payloadBytes,
	}

	encoder := json.NewEncoder(c.conn)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	var resp Response
	if err := newLimitedDecoder(c.reader).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("store vm error: internal error")
	}

	if resp.Data == nil {
		return nil, nil
	}

	return resp.Data, nil
}

func newLimitedDecoder(r io.Reader) *json.Decoder {
	return json.NewDecoder(io.LimitReader(r, MaxPayloadLen+1))
}

// --- Proposal Store Implementation ---

func (r *ProposalStoreImpl) Create(p *proposal.Proposal) error {
	data, err := r.client.sendRequest("proposal.create", p)
	if err != nil {
		return err
	}
	if data == nil {
		return nil
	}
	var created proposal.Proposal
	if err := json.Unmarshal(data, &created); err != nil {
		return fmt.Errorf("unmarshal proposal create: %w", err)
	}
	*p = created
	return nil
}

func (r *ProposalStoreImpl) Get(id string) (*proposal.Proposal, error) {
	data, err := r.client.sendRequest("proposal.get", map[string]string{"id": id})
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("proposal not found: %s", id)
	}
	var p proposal.Proposal
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal proposal get: %w", err)
	}
	return &p, nil
}

func (r *ProposalStoreImpl) Update(p *proposal.Proposal) error {
	_, err := r.client.sendRequest("proposal.update", p)
	return err
}

func (r *ProposalStoreImpl) List() ([]proposal.ProposalSummary, error) {
	data, err := r.client.sendRequest("proposal.list", nil)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var summaries []proposal.ProposalSummary
	if err := json.Unmarshal(data, &summaries); err != nil {
		return nil, fmt.Errorf("unmarshal proposal list: %w", err)
	}
	return summaries, nil
}

func (r *ProposalStoreImpl) ListByStatus(status proposal.Status) ([]proposal.ProposalSummary, error) {
	data, err := r.client.sendRequest("proposal.list_by_status", map[string]string{"status": string(status)})
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var summaries []proposal.ProposalSummary
	if err := json.Unmarshal(data, &summaries); err != nil {
		return nil, fmt.Errorf("unmarshal proposal list by status: %w", err)
	}
	return summaries, nil
}

func (r *ProposalStoreImpl) ResolveID(prefix string) (string, error) {
	data, err := r.client.sendRequest("proposal.resolve_id", map[string]string{"prefix": prefix})
	if err != nil {
		return "", err
	}
	if data == nil {
		return "", fmt.Errorf("no ID resolved for prefix: %s", prefix)
	}
	var resolved string
	if err := json.Unmarshal(data, &resolved); err != nil {
		return "", fmt.Errorf("unmarshal proposal resolve id: %w", err)
	}
	return resolved, nil
}

func (r *ProposalStoreImpl) Import(p *proposal.Proposal) error {
	_, err := r.client.sendRequest("proposal.import", p)
	return err
}

// --- Other Store Stubs (Minimal implementations to satisfy interfaces) ---

func (r *MemoryStoreImpl) Store(entry *memory.MemoryEntry) (string, error) {
	_, err := r.client.sendRequest("memory.store", entry)
	return "", err
}
func (r *MemoryStoreImpl) Retrieve(query string, k int, taskID string) ([]*memory.MemoryEntry, error) {
	return nil, fmt.Errorf("remote memory retrieve is not implemented for %q/%d/%q", query, k, taskID)
}
func (r *MemoryStoreImpl) List(tier memory.TTLTier) ([]memory.StoreSummary, error) {
	return nil, fmt.Errorf("remote memory list is not implemented for tier %s", tier)
}

func (r *PullRequestStoreImpl) Create(pr *pullrequest.PullRequest) error { return nil }
func (r *PullRequestStoreImpl) Get(id string) (*pullrequest.PullRequest, error) {
	return nil, fmt.Errorf("remote pull request get is not implemented for %s", id)
}
func (r *PullRequestStoreImpl) GetByProposalID(proposalID string) (*pullrequest.PullRequest, error) {
	return nil, fmt.Errorf("remote pull request get by proposal is not implemented for %s", proposalID)
}
func (r *PullRequestStoreImpl) List(status *pullrequest.Status) ([]*pullrequest.PullRequest, error) {
	return nil, fmt.Errorf("remote pull request list is not implemented")
}
func (r *PullRequestStoreImpl) Update(pr *pullrequest.PullRequest) error { return nil }
func (r *PullRequestStoreImpl) Approve(prID, approvedBy string) error    { return nil }
func (r *PullRequestStoreImpl) Close(prID string) error                  { return nil }
func (r *PullRequestStoreImpl) MarkMerged(prID string) error             { return nil }

func (r *CompositionStoreImpl) Publish(components map[string]composition.Component, actor, reason string) (*composition.Manifest, error) {
	return nil, fmt.Errorf("remote composition publish is not implemented")
}
func (r *CompositionStoreImpl) Current() *composition.Manifest { return nil }
func (r *CompositionStoreImpl) Get(version int) (*composition.Manifest, error) {
	return nil, fmt.Errorf("remote composition get is not implemented for v%d", version)
}

func (r *WorkerStoreImpl) Upsert(record *worker.WorkerRecord) error    { return nil }
func (r *WorkerStoreImpl) Get(id string) (*worker.WorkerRecord, bool)  { return nil, false }
func (r *WorkerStoreImpl) List(activeOnly bool) []*worker.WorkerRecord { return nil }
