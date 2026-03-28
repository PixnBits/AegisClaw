// AegisHub is the system IPC router microVM for AegisClaw.
//
// Security model
// ──────────────
// AegisHub runs inside a Firecracker microVM with the same isolation guarantees
// as every other AegisClaw component: read-only rootfs, cap-drop ALL, no shared
// memory, vsock-only external communication. It is the SOLE routing authority
// for all inter-VM traffic. No VM may communicate with another VM directly.
//
// Boundary protocol
// ─────────────────
// The host daemon communicates with AegisHub exclusively over vsock (AF_VSOCK,
// CID 2 → port 1024 inside the VM). Every message is a JSON object with a
// "type" discriminator and a "payload" field. AegisHub enforces the ACL policy
// and identity registry before any message is delivered.
//
// Supported message types (received from daemon):
//
//	hub.register_vm    — Associate a VM ID with its access-control role.
//	hub.unregister_vm  — Remove a VM identity on shutdown.
//	hub.route          — Route an IPC message on behalf of a VM.
//	hub.status         — Return hub health statistics.
//
// Updates to AegisHub itself must flow through the Governance Court SDLC with a
// signed composition manifest; no direct operator modification is permitted.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PixnBits/AegisClaw/internal/ipc"
	"go.uber.org/zap"
)

const (
	// vsockPort is the well-known port AegisHub listens on inside the VM.
	// The host daemon connects to this port via the Firecracker vsock UDS.
	vsockPort = 1024

	// maxPayloadLen caps incoming message size to prevent memory exhaustion.
	maxPayloadLen = 4 * 1024 * 1024 // 4 MiB
)

// HubRequest is the envelope the daemon sends to AegisHub.
type HubRequest struct {
	// ID is an opaque correlation token echoed in the response.
	ID string `json:"id"`
	// Type is the operation discriminator (hub.register_vm, hub.route, etc.).
	Type string `json:"type"`
	// Payload is the operation-specific data.
	Payload json.RawMessage `json:"payload"`
}

// HubResponse is AegisHub's reply to a HubRequest.
type HubResponse struct {
	// ID matches the request ID for correlation.
	ID string `json:"id"`
	// Success indicates whether the operation succeeded.
	Success bool `json:"success"`
	// Error carries a human-readable error string on failure.
	Error string `json:"error,omitempty"`
	// Data carries operation-specific response data on success.
	Data json.RawMessage `json:"data,omitempty"`
}

// RegisterVMPayload is the payload for hub.register_vm.
type RegisterVMPayload struct {
	VMID string      `json:"vm_id"`
	Role ipc.VMRole  `json:"role"`
}

// UnregisterVMPayload is the payload for hub.unregister_vm.
type UnregisterVMPayload struct {
	VMID string `json:"vm_id"`
}

// RoutePayload is the payload for hub.route.
type RoutePayload struct {
	// SenderVMID is the vsock-verified identity of the originating VM.
	// This is set by the daemon after verifying the vsock connection identity —
	// it is NOT taken from the message's "from" field to prevent spoofing.
	SenderVMID string      `json:"sender_vm_id"`
	// Message is the IPC envelope to route.
	Message    ipc.Message `json:"message"`
}

// RouteResult is the data field of a successful hub.route response.
// It contains both the delivery outcome and, when applicable, instructions
// for the daemon to forward a follow-up message to another VM.
type RouteResult struct {
	// DeliveryResult is the direct outcome of the routing attempt.
	DeliveryResult *ipc.DeliveryResult `json:"delivery_result"`
	// DeliverToVM is set when AegisHub needs the daemon to forward a message
	// to a specific VM after ACL and routing checks. Empty when the result
	// was produced locally (e.g. hub.status reply).
	DeliverToVM string `json:"deliver_to_vm,omitempty"`
	// ForwardMessage is the forwarded envelope the daemon should send to
	// DeliverToVM. Only present when DeliverToVM is non-empty.
	ForwardMessage *ipc.Message `json:"forward_message,omitempty"`
}

func main() {
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("aegishub: failed to create logger: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	hub := ipc.NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		logger.Fatal("aegishub: failed to start message hub", zap.Error(err))
	}
	defer hub.Stop()

	logger.Info("AegisHub started",
		zap.String("role", "system-ipc-router"),
		zap.String("listen", fmt.Sprintf("vsock::%d", vsockPort)),
	)

	listener, err := listenVsock(vsockPort)
	if err != nil {
		logger.Fatal("aegishub: failed to listen on vsock", zap.Error(err))
	}
	defer listener.Close()

	// Graceful shutdown on SIGTERM / SIGINT.
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigC
		logger.Info("AegisHub received shutdown signal")
		listener.Close()
	}()

	srv := &server{hub: hub, logger: logger}
	srv.serve(listener)
}

// server processes incoming daemon connections.
type server struct {
	hub    *ipc.MessageHub
	logger *zap.Logger
}

func (s *server) serve(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			// Listener was closed — normal shutdown path.
			return
		}
		go s.handleConn(conn)
	}
}

func (s *server) handleConn(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(io.LimitReader(conn, maxPayloadLen))
	decoder := json.NewDecoder(reader)
	encoder := json.NewEncoder(conn)

	for {
		if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
			s.logger.Warn("failed to set connection deadline", zap.Error(err))
			return
		}

		var req HubRequest
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF {
				return
			}
			s.sendErr(encoder, "", fmt.Sprintf("decode error: %v", err))
			return
		}

		resp := s.dispatch(&req)
		if err := encoder.Encode(resp); err != nil {
			s.logger.Error("failed to send hub response",
				zap.String("req_id", req.ID),
				zap.Error(err),
			)
			return
		}
	}
}

func (s *server) dispatch(req *HubRequest) *HubResponse {
	switch req.Type {
	case "hub.register_vm":
		return s.handleRegisterVM(req)
	case "hub.unregister_vm":
		return s.handleUnregisterVM(req)
	case "hub.route":
		return s.handleRoute(req)
	case "hub.status":
		return s.handleStatus(req)
	default:
		return &HubResponse{
			ID:    req.ID,
			Error: fmt.Sprintf("unknown request type: %q", req.Type),
		}
	}
}

func (s *server) handleRegisterVM(req *HubRequest) *HubResponse {
	var p RegisterVMPayload
	if err := json.Unmarshal(req.Payload, &p); err != nil {
		return errResponse(req.ID, fmt.Sprintf("invalid payload: %v", err))
	}
	if p.VMID == "" {
		return errResponse(req.ID, "vm_id is required")
	}
	if p.Role == "" {
		return errResponse(req.ID, "role is required")
	}

	if err := s.hub.RegisterVM(p.VMID, p.Role); err != nil {
		return errResponse(req.ID, err.Error())
	}

	s.logger.Info("VM registered with AegisHub",
		zap.String("vm_id", p.VMID),
		zap.String("role", string(p.Role)),
	)
	return &HubResponse{ID: req.ID, Success: true}
}

func (s *server) handleUnregisterVM(req *HubRequest) *HubResponse {
	var p UnregisterVMPayload
	if err := json.Unmarshal(req.Payload, &p); err != nil {
		return errResponse(req.ID, fmt.Sprintf("invalid payload: %v", err))
	}
	if p.VMID == "" {
		return errResponse(req.ID, "vm_id is required")
	}

	s.hub.UnregisterVM(p.VMID)
	s.logger.Info("VM unregistered from AegisHub", zap.String("vm_id", p.VMID))
	return &HubResponse{ID: req.ID, Success: true}
}

func (s *server) handleRoute(req *HubRequest) *HubResponse {
	var p RoutePayload
	if err := json.Unmarshal(req.Payload, &p); err != nil {
		return errResponse(req.ID, fmt.Sprintf("invalid payload: %v", err))
	}
	if p.SenderVMID == "" {
		return errResponse(req.ID, "sender_vm_id is required")
	}

	result, err := s.hub.RouteMessage(p.SenderVMID, &p.Message)
	if err != nil {
		return errResponse(req.ID, err.Error())
	}

	// Determine whether the daemon needs to forward a message to another VM.
	// If the destination is not the hub itself and the route resolved
	// successfully, the daemon must deliver the message to that VM.
	var deliverToVM string
	var forwardMsg *ipc.Message
	if result.Success && p.Message.To != ipc.MessageHubID {
		deliverToVM = p.Message.To
		forwardMsg = &p.Message
	}

	rr := RouteResult{
		DeliveryResult: result,
		DeliverToVM:    deliverToVM,
		ForwardMessage: forwardMsg,
	}
	data, _ := json.Marshal(rr)
	return &HubResponse{ID: req.ID, Success: true, Data: data}
}

func (s *server) handleStatus(req *HubRequest) *HubResponse {
	stats := s.hub.Stats()
	data, _ := json.Marshal(map[string]interface{}{
		"state":             string(s.hub.State()),
		"messages_routed":   stats.MessagesRouted,
		"messages_rejected": stats.MessagesRejected,
		"delivery_errors":   stats.DeliveryErrors,
		"started_at":        stats.StartedAt,
		"routes":            s.hub.Router().RegisteredRoutes(),
	})
	return &HubResponse{ID: req.ID, Success: true, Data: data}
}

func (s *server) sendErr(enc *json.Encoder, id, msg string) {
	resp := &HubResponse{ID: id, Error: msg}
	enc.Encode(resp) //nolint:errcheck
}

func errResponse(id, msg string) *HubResponse {
	return &HubResponse{ID: id, Error: msg}
}

// listenVsock creates a vsock listener on the given port.
// Inside a Firecracker VM, AF_VSOCK is available; on the host we fall back
// to a TCP port for integration tests.
func listenVsock(port uint32) (net.Listener, error) {
	l, err := listenAFVsock(port)
	if err == nil {
		return l, nil
	}
	// Fallback for test environments where AF_VSOCK is not available.
	return net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
}
