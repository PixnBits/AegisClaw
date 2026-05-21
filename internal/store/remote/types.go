package remote

// Phase 2.8: Basic protocol types for Store VM communication over vsock.
// Using simple JSON for initial implementation. Can evolve to protobuf or custom binary later.

// Request is sent from client (AegisHub/daemon) to Store VM.
type Request struct {
	ID      string          `json:"id"`
	Op      string          `json:"op"`      // e.g. "proposal.create", "memory.store", "list_proposals"
	Payload interface{}     `json:"payload,omitempty"`
}

// Response is returned by the Store VM.
type Response struct {
	ID      string          `json:"id"`
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    interface{}     `json:"data,omitempty"`
}

// Common operations
const (
	OpProposalCreate   = "proposal.create"
	OpProposalGet      = "proposal.get"
	OpProposalList     = "proposal.list"
	OpMemoryStore      = "memory.store"
	OpMemorySearch     = "memory.search"
	// Add more as needed
)
