// Package hubclient provides a reusable, paranoid client for communicating with AegisHub.
// It supports both Unix domain sockets (for host-side components and dev) and vsock
// (for real Firecracker microVM guests: agent runtime, memory VM, etc.).
//
// This is the foundation for Phase 1 (Core Runtime) per the No-Stubs-Left plan.
// All inter-component traffic (including the 6-step loop in agent-runtime.md) must
// eventually flow through this or equivalent after the handshake.
//
// SPEC REFERENCES (cited in every commit and hot-path comment):
//   - docs/specs/aegishub.md §Handshake Sequence (steps 1-4: "MicroVM connects to AegisHub via vsock",
//     register with public key, receive ACLs).
//   - docs/specs/aegishub.md §Message Format + §Authentication (Ed25519 signatures on every message after register).
//   - docs/specs/agent-runtime.md §Communication ("Only allowed interfaces: vsock / JSON-RPC to AegisHub").
//   - docs/specs/agent-runtime.md §Security & Isolation (paranoid model, no direct VM-to-VM).
//   - docs/prd/security-model.md §Communication & Mediation + §Isolation Strategy (every boundary is a boundary;
//     fail-closed, least-privilege, audit everything).
//   - docs/prd/runtime-architecture.md (Agent Runtime VMs + Memory VM talk only via the privileged router).
//   - docs/no-stubs-left-resolution-plan.md §Phase 1 + docs/no-stubs-plan/phase-1.md (1.1a transport foundation;
//     remove all surface unix-only + mock paths from agent execution).
//   - AGENTS.md (verification discipline, no unauthorized daemon lifecycle).
//
// Design goals (paranoid):
//   - Fail closed on any signature/ACL/handshake error.
//   - Private key material is never logged and is zeroed at the earliest safe point by the caller.
//   - Context-aware with deadlines; no indefinite blocking.
//   - Wire-compatible with existing Message used by hub/agent/memory (exact JSON shape).
//   - Usable immediately over unix for dev; vsock path ready for guests (host CID 2 convention matches orchestrator.go).
package hubclient

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/mdlayher/vsock"
)

// Message is the exact wire format for all traffic to/from AegisHub (JSON-RPC style).
// It MUST remain byte-for-byte compatible with the definitions in:
//   - cmd/aegishub/main.go:34
//   - cmd/agent/main.go:20
//   - cmd/memory/main.go:22
// Any change requires coordinated updates and spec revision.
//
// All messages after the initial "register" MUST carry a valid Ed25519 signature
// (see signMessage logic and aegishub.md §Authentication).
type Message struct {
	Source      string      `json:"source"`
	Destination string      `json:"destination"`
	Command     string      `json:"command"`
	Payload     interface{} `json:"payload"`
	Timestamp   string      `json:"timestamp"`
	Signature   string      `json:"signature"`
}

// Well-known constants for vsock guest<->hub control plane.
// These are chosen to be distinct from per-VM egress vsock ports (9000+ allocated in orchestrator).
// The host CID 2 matches the documented convention in internal/runtime/orchestrator.go:127
// ("vsock://2:8081" for Network Boundary egress).
const (
	// HubVsockPort is the fixed port AegisHub will listen on for guest microVM connections (vsock).
	// Guests (agent, memory, court personas, etc.) dial this from inside Firecracker.
	// Documented in aegishub.md implementation notes (to be added in 1.4).
	HubVsockPort = 9999

	// HostCID is the CID that guests use to reach the host over vsock.
	// This is the established Firecracker + vsock convention used elsewhere in the tree.
	HostCID = 2

	// LogVsockPort (Phase 1 microvm-observability) is the dedicated port on which
	// guests emit structured logs to the Host Daemon over vsock. This provides
	// reliable application-level visibility (startup events, vsock listener status,
	// errors) that is independent of fragile serial console capture.
	LogVsockPort = 18099

	// PortalBridgeVsockPort is the host listener for the Web Portal microVM when
	// direct guest→hub vsock is unavailable. The daemon forwards actions to Hub/backends.
	// See docs/specs/web-portal/implementation-current.md (portalAPIClient dials host bridge vsock 1030).
	PortalBridgeVsockPort = 1030

	// GuestHubBridgePort is used on Firecracker when guest→host AF_VSOCK is unavailable
	// (connection reset / ENODEV). The guest listens on this port; the Host Daemon dials
	// in via the Firecracker hybrid-vsock UDS (CONNECT handshake) and bridges bytes to
	// the AegisHub unix socket. Same pattern as web-portal :18080 but inverted direction.
	GuestHubBridgePort = 9101

	// OllamaBridgeGuestPort is the vsock port on which the network-boundary guest listens
	// for the host Ollama bridge (inverted path — same pattern as GuestHubBridgePort).
	// Firecracker guests cannot dial host vsock directly; the host dials in via the
	// hybrid-vsock UDS when llm.call needs real Ollama on the host.
	OllamaBridgeGuestPort = 9102
)

// Sentinel errors for AegisHub protocol responses (exact strings from aegishub/main.go).
// The client maps raw "error" responses to these for fail-closed handling by callers.
// Callers (agent steps, memory, etc.) must treat any of these as hard failures (no fallback).
var (
	ErrInvalidHandshake   = errors.New("ERR_INVALID_HANDSHAKE")
	ErrInvalidPayload     = errors.New("ERR_INVALID_PAYLOAD")
	ErrMissingPublicKey   = errors.New("ERR_MISSING_PUBLIC_KEY")
	ErrInvalidPublicKey   = errors.New("ERR_INVALID_PUBLIC_KEY")
	ErrDuplicateComponent = errors.New("ERR_DUPLICATE_COMPONENT")
	ErrUnauthorized       = errors.New("ERR_UNAUTHORIZED")
	ErrSignatureRequired  = errors.New("ERR_SIGNATURE_REQUIRED")
	ErrInvalidSignature   = errors.New("ERR_INVALID_SIGNATURE")
	ErrACLViolation         = errors.New("ERR_ACL_VIOLATION")
	ErrDestinationNotFound  = errors.New("ERR_DESTINATION_NOT_FOUND")
	ErrRPCTimeout           = errors.New("ERR_RPC_TIMEOUT")
	ErrUnknown              = errors.New("ERR_UNKNOWN")
)

// RegisterResponse is returned after a successful "register" handshake (aegishub.md §Handshake Sequence).
// The AssignedID is the authoritative identity the component must use in subsequent Source fields.
// ACLs are the rules the hub will enforce for this component (hot-reloadable on the hub side).
type RegisterResponse struct {
	Status     string        `json:"status"`
	AssignedID string        `json:"assigned_id"`
	ACLs       []interface{} `json:"acls"`
	Version    string        `json:"version,omitempty"`
}

// Client is the primary interface for all AegisHub communication from sandboxes and host components.
// Implementations guarantee:
//   - Every outgoing Message (post-Register) is signed with the component's private key.
//   - Timeouts and cancellation via context.
//   - No plaintext private key material escapes the Dial call site (caller responsibility to zero).
//   - Connection is exclusive to one component identity.
//
// This interface is the "vsock client to AegisHub" required by the user query and plan 1.1a.
type Client interface {
	// Register performs the mandatory first step of the handshake (aegishub.md).
	// It must be the very first message sent on a new connection.
	// Returns the hub-assigned ID and a snapshot of applicable ACLs.
	// After Register succeeds, all further Send calls will use the AssignedID as Source if not already set.
	Register(ctx context.Context, componentID string, pub ed25519.PublicKey, version string) (*RegisterResponse, error)

	// Send sends one signed message and blocks until a response or error is received (or ctx done).
	// The caller populates Source (usually the AssignedID), Destination, Command, Payload, Timestamp.
	// This method fills in the Signature using the private key captured at Dial time.
	// Returns the hub's reply (which may itself be an error response — check Command == "error").
	Send(ctx context.Context, msg Message) (Message, error)

	// Reply sends a one-way signed message without waiting for a hub reply.
	// Long-lived components (agent, memory) must use this for inbound RPC responses;
	// using Send for replies deadlocks the shared JSON decoder with Receive.
	Reply(ctx context.Context, msg Message) error

	// Close releases the underlying transport (unix or vsock).
	Close() error

	// AssignedID returns the ID granted by the hub during Register, or "" if not yet registered.
	AssignedID() string

	// IsVsock reports whether this client is using the vsock transport (useful for logging/audit).
	IsVsock() bool

	// Receive reads the next incoming message from the hub.
	// This is required for long-lived components (Agent Runtime, Memory VM) that
	// need to receive pushed messages (user turns, autonomy grants, background work,
	// skill updates, etc.) in addition to replying to Send calls.
	//
	// The hubclient maintains the underlying connection; callers should typically
	// run Receive in a loop or with select on context.
	//
	// SPEC: agent-runtime.md §Communication + §Key Interfaces (event subscription
	// for user messages and court feedback); aegishub.md (bidirectional signed JSON-RPC).
	Receive(ctx context.Context) (Message, error)

	// TryReceive waits up to timeout for an inbound message. ok=false on timeout (not an error).
	TryReceive(ctx context.Context, timeout time.Duration) (Message, bool, error)
}

// dialer is an internal seam for testability (real net.Dial vs. vsock.Dial vs. net.Pipe in tests).
type dialer func() (net.Conn, error)

// client is the concrete implementation of Client.
// It is not safe for concurrent Send from multiple goroutines unless externally synchronized
// (callers such as the agent loop are expected to be single-threaded per turn or use their own locking).
type client struct {
	conn       net.Conn
	enc        *json.Encoder
	dec        *json.Decoder
	decMu      sync.Mutex
	priv       ed25519.PrivateKey // captured at Dial; caller MUST zero their original copy immediately after Dial
	assignedID string
	isVsock    bool
}

// signMessage signs the message in place using the same canonical logic as cmd/agent/main.go:68
// and cmd/memory/main.go:63 (and the inverse of hub's verifySignature).
// This ensures wire compatibility.
// SECURITY: the caller of this helper must have already zeroed any other copies of priv.
func signMessage(msg *Message, priv ed25519.PrivateKey) {
	msgCopy := *msg
	msgCopy.Signature = ""
	data, _ := json.Marshal(msgCopy) // best-effort; real errors surface at Send time
	signature := ed25519.Sign(priv, data)
	msg.Signature = base64.StdEncoding.EncodeToString(signature)
}

// mapHubError converts a raw error string from the hub into one of our sentinel errors.
// Unknown errors become ErrUnknown (fail-closed).
func mapHubError(errStr string) error {
	switch errStr {
	case "ERR_INVALID_HANDSHAKE":
		return ErrInvalidHandshake
	case "ERR_INVALID_PAYLOAD":
		return ErrInvalidPayload
	case "ERR_MISSING_PUBLIC_KEY":
		return ErrMissingPublicKey
	case "ERR_INVALID_PUBLIC_KEY":
		return ErrInvalidPublicKey
	case "ERR_DUPLICATE_COMPONENT":
		return ErrDuplicateComponent
	case "ERR_UNAUTHORIZED":
		return ErrUnauthorized
	case "ERR_SIGNATURE_REQUIRED":
		return ErrSignatureRequired
	case "ERR_INVALID_SIGNATURE":
		return ErrInvalidSignature
	case "ERR_ACL_VIOLATION":
		return ErrACLViolation
	case "ERR_DESTINATION_NOT_FOUND":
		return ErrDestinationNotFound
	case "ERR_RPC_TIMEOUT":
		return ErrRPCTimeout
	default:
		if strings.HasPrefix(errStr, "ERR_ACL_VIOLATION") {
			return ErrACLViolation
		}
		return ErrUnknown
	}
}

// DialUnix creates a new Client connected to AegisHub over a Unix domain socket (dev / host components).
// The provided private key is copied internally; the caller MUST zero their copy of priv immediately
// after this call returns (paranoid key hygiene per security-model.md and orchestrator key distribution).
func DialUnix(socketPath string, priv ed25519.PrivateKey) (Client, error) {
	if socketPath == "" {
		return nil, errors.New("hubclient: empty unix socket path")
	}
	// Paranoid fail-closed: validate key material before any network activity or resource allocation.
	if len(priv) != ed25519.PrivateKeySize {
		return nil, ErrInvalidPublicKey
	}
	d := func() (net.Conn, error) {
		return net.Dial("unix", socketPath)
	}
	return newClientFromDialer(d, priv, false)
}

// DialVsock creates a new Client connected to AegisHub over vsock (real Firecracker microVM guests).
// cid is almost always HostCID (2). port is almost always HubVsockPort (9999).
// This is the path that satisfies agent-runtime.md "vsock / JSON-RPC to AegisHub" for the 6-step loop
// when the Agent Runtime VM is running inside a Firecracker sandbox.
//
// The private key handling contract is identical to DialUnix (caller zeros after return).
func DialVsock(cid uint32, port uint32, priv ed25519.PrivateKey) (Client, error) {
	// Paranoid fail-closed: validate key material before any network activity or resource allocation.
	if len(priv) != ed25519.PrivateKeySize {
		return nil, ErrInvalidPublicKey
	}
	d := func() (net.Conn, error) {
		// Use the library's exported Host constant (== 2) or the caller's explicit cid.
		// This matches the convention used for Boundary egress (orchestrator.go:127 "vsock://2:8081").
		// See docs/specs/aegishub.md §Handshake Sequence for the guest vsock requirement.
		effectiveCID := cid
		if effectiveCID == 0 {
			effectiveCID = vsock.Host
		}
		return vsock.Dial(effectiveCID, port, nil)
	}
	return newClientFromDialer(d, priv, true)
}

// AcceptVsockHubBridge waits for the Host Daemon to connect over Firecracker's inverted
// hub bridge (guest listens, host dials via fc-*-vsock.sock CONNECT). Use this instead
// of DialVsock when aegis.hub_vsock=1 inside a Firecracker guest.
func AcceptVsockHubBridge(port uint32, priv ed25519.PrivateKey) (Client, error) {
	if port == 0 {
		port = GuestHubBridgePort
	}
	if len(priv) != ed25519.PrivateKeySize {
		return nil, ErrInvalidPublicKey
	}
	ln, err := vsock.Listen(port, nil)
	if err != nil {
		return nil, fmt.Errorf("hubclient: vsock listen on port %d: %w", port, err)
	}
	defer ln.Close()

	conn, err := ln.Accept()
	if err != nil {
		return nil, fmt.Errorf("hubclient: vsock accept on port %d: %w", port, err)
	}
	return newClientFromConn(conn, priv, true)
}

// AcceptVsockHubBridgeConn waits for the host hub bridge and returns the raw connection.
// Use when the component speaks the hub JSON protocol directly (store, network-boundary).
func AcceptVsockHubBridgeConn(port uint32) (net.Conn, error) {
	if port == 0 {
		port = GuestHubBridgePort
	}
	ln, err := vsock.Listen(port, nil)
	if err != nil {
		return nil, fmt.Errorf("hubclient: vsock listen on port %d: %w", port, err)
	}
	defer ln.Close()
	conn, err := ln.Accept()
	if err != nil {
		return nil, fmt.Errorf("hubclient: vsock accept on port %d: %w", port, err)
	}
	return conn, nil
}

// newClientFromConn wraps an established connection (used by AcceptVsockHubBridge).
func newClientFromConn(conn net.Conn, priv ed25519.PrivateKey, isVsock bool) (Client, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, ErrInvalidPublicKey
	}
	c := &client{
		conn:    conn,
		enc:     json.NewEncoder(conn),
		dec:     json.NewDecoder(conn),
		priv:    make([]byte, ed25519.PrivateKeySize),
		isVsock: isVsock,
	}
	copy(c.priv, priv)
	return c, nil
}

// newClientFromDialer is the common constructor. It performs the low-level dial and wraps the conn.
func newClientFromDialer(d dialer, priv ed25519.PrivateKey, isVsock bool) (Client, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, ErrInvalidPublicKey // reuse for "bad key material" (fail closed)
	}

	conn, err := d()
	if err != nil {
		return nil, err
	}
	return newClientFromConn(conn, priv, isVsock)
}

// Register implements the mandatory handshake (aegishub.md §Handshake Sequence).
// It must be the first operation after Dial. On success the client is considered authenticated
// for subsequent Send calls and stores the assigned ID.
func (c *client) Register(ctx context.Context, componentID string, pub ed25519.PublicKey, version string) (*RegisterResponse, error) {
	if len(c.priv) == 0 {
		return nil, errors.New("hubclient: client has no private key (already closed or misused)")
	}
	if componentID == "" {
		return nil, errors.New("hubclient: componentID required for register")
	}

	reg := Message{
		Source:      componentID,
		Destination: "hub",
		Command:     "register",
		Payload: map[string]string{
			"public_key": base64.StdEncoding.EncodeToString(pub),
			"version":    version,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Signature: "dummy", // allowed only for the register handshake (hub special-cases it)
	}
	// For register we deliberately allow "dummy" (matching current hub behavior in dev).
	// Real traffic after register is strictly signed.

	// Send without the normal signing path (register is special)
	if err := c.sendRaw(ctx, reg); err != nil {
		return nil, err
	}

	var rawResp map[string]interface{}
	if err := c.decodeWithCtx(ctx, &rawResp); err != nil {
		return nil, err
	}

	// Hub returns either an error map or the RegisterResponse shape
	if errStr, ok := rawResp["error"].(string); ok && errStr != "" {
		return nil, mapHubError(errStr)
	}

	resp := &RegisterResponse{}
	if status, ok := rawResp["status"].(string); ok {
		resp.Status = status
	}
	if id, ok := rawResp["assigned_id"].(string); ok {
		resp.AssignedID = id
		c.assignedID = id
	}
	if acls, ok := rawResp["acls"].([]interface{}); ok {
		resp.ACLs = acls
	}
	if v, ok := rawResp["version"].(string); ok {
		resp.Version = v
	}

	if resp.Status != "registered" {
		return nil, ErrInvalidHandshake
	}
	return resp, nil
}

// Send implements Client.Send. It signs the message (unless it is a register, which should not come here)
// and performs a request/reply exchange.
func (c *client) Send(ctx context.Context, msg Message) (Message, error) {
	if c.assignedID == "" {
		// Enforce that Register happened first (paranoid, matches hub handshake requirement)
		return Message{}, errors.New("hubclient: Register must succeed before any Send")
	}
	if len(c.priv) == 0 {
		return Message{}, errors.New("hubclient: client closed or key material cleared")
	}

	// Ensure the message is signed with our captured private key
	signMessage(&msg, c.priv)

	if err := c.sendRaw(ctx, msg); err != nil {
		return Message{}, err
	}

	for {
		var resp Message
		if err := c.decodeWithCtx(ctx, &resp); err != nil {
			return Message{}, err
		}

		// Hub ack after a one-way Reply was delivered; not the RPC response we are waiting for.
		if resp.Command == "ack" {
			continue
		}

		if resp.Command == "error" {
			if p, ok := resp.Payload.(map[string]interface{}); ok {
				if es, ok := p["error"].(string); ok {
					return resp, mapHubError(es)
				}
			}
			if es, ok := resp.Payload.(string); ok {
				return resp, mapHubError(es)
			}
			return resp, ErrUnknown
		}

		return resp, nil
	}
}

// Reply implements Client.Reply — fire-and-forget outbound message (no decode).
func (c *client) Reply(ctx context.Context, msg Message) error {
	if c.assignedID == "" {
		return errors.New("hubclient: Register must succeed before any Reply")
	}
	if len(c.priv) == 0 {
		return errors.New("hubclient: client closed or key material cleared")
	}
	signMessage(&msg, c.priv)
	return c.sendRaw(ctx, msg)
}

// sendRaw writes a Message without additional signing (used internally by Register and by Send after it signs).
func (c *client) sendRaw(ctx context.Context, msg Message) error {
	// Best-effort deadline from context
	if dl, ok := ctx.Deadline(); ok {
		_ = c.conn.SetWriteDeadline(dl)
		defer c.conn.SetWriteDeadline(time.Time{})
	}

	if err := c.enc.Encode(msg); err != nil {
		return err
	}
	return nil
}

// decodeWithCtx reads the next JSON object, respecting context cancellation/deadline.
func (c *client) decodeWithCtx(ctx context.Context, v interface{}) error {
	type decodeResult struct {
		err error
	}
	resCh := make(chan decodeResult, 1)

	go func() {
		c.decMu.Lock()
		defer c.decMu.Unlock()
		err := c.dec.Decode(v)
		resCh <- decodeResult{err: err}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-resCh:
		return r.err
	}
}

// Receive implements Client.Receive for long-lived components.
func (c *client) Receive(ctx context.Context) (Message, error) {
	if c.conn == nil {
		return Message{}, errors.New("hubclient: connection closed")
	}

	var msg Message
	if err := c.decodeWithCtx(ctx, &msg); err != nil {
		return Message{}, err
	}
	return msg, nil
}

func (c *client) TryReceive(ctx context.Context, timeout time.Duration) (Message, bool, error) {
	if c.conn == nil {
		return Message{}, false, errors.New("hubclient: connection closed")
	}
	if timeout <= 0 {
		timeout = 50 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	if err := c.conn.SetReadDeadline(deadline); err != nil {
		return Message{}, false, err
	}
	defer c.conn.SetReadDeadline(time.Time{}) //nolint:errcheck

	var msg Message
	c.decMu.Lock()
	err := c.dec.Decode(&msg)
	c.decMu.Unlock()
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return Message{}, false, nil
		}
		return Message{}, false, err
	}
	return msg, true, nil
}

// Close implements Client.Close. It also attempts best-effort zeroization of the private key material
// held by the client (defense in depth).
func (c *client) Close() error {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	// Zero the captured private key bytes
	for i := range c.priv {
		c.priv[i] = 0
	}
	c.priv = nil
	c.assignedID = ""
	return nil
}

func (c *client) AssignedID() string {
	return c.assignedID
}

func (c *client) IsVsock() bool {
	return c.isVsock
}

// ZeroPrivateKey is a convenience helper callers can use on their own private key variable
// immediately after passing it to Dial* (paranoid hygiene).
// It is not strictly required if the caller already zeros, but makes the contract obvious.
func ZeroPrivateKey(priv ed25519.PrivateKey) {
	for i := range priv {
		priv[i] = 0
	}
}
