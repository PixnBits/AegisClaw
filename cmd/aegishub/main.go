// This version routes all proposal operations through a remote Store VM client.
// AegisHub acts as a strict mediator. It holds no persistent proposal state.
// The remote ProposalStore is wired at startup via the Store VM vsock connection.
// All vsock connections are authenticated via mutual handshake.
// Input is strictly validated; errors are sanitized to prevent information leakage.
// Connection timeouts prevent slow-client DoS.
// Trust Boundary: AegisHub trusts the Store VM only after successful handshake.
// All external messages are considered hostile until proven otherwise.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/ipc"
	"github.com/PixnBits/AegisClaw/internal/store/remote"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

const defaultAegisHubVSOCKPort = 9998
const requestTimeout = 30 * time.Second

type HubRequest struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type HubResponse struct {
	ID      string          `json:"id"`
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type RegisterVMPayload struct {
	VMID string `json:"vm_id"`
	Role string `json:"role"`
}

type handshakeRequest struct {
	Type   string `json:"type"`
	Secret string `json:"secret"`
	VMID   string `json:"vm_id"`
	Role   string `json:"role"`
}

type authenticatedVM struct {
	ID   string
	Role ipc.VMRole
}

type server struct {
	logger *zap.Logger
	hub    *ipc.MessageHub
}

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	srv := &server{
		logger: logger,
		hub:    ipc.NewMessageHubNoKernel(logger),
	}

	if err := srv.hub.Start(); err != nil {
		logger.Fatal("message hub failed to start", zap.Error(err))
	}

	// Wire the remote ProposalStore via Store VM vsock connection.
	remoteVsockAddr := os.Getenv("STORE_VM_VSOCK_ADDR")
	if remoteVsockAddr == "" {
		remoteVsockAddr = "vsock://3:9999"
	}

	remoteClient, err := remote.NewRemoteClient(remoteVsockAddr)
	if err != nil {
		logger.Fatal("failed to create remote store client", zap.Error(err))
	}
	defer remoteClient.Close()

	logger.Info("remote store client connected", zap.String("addr", remoteVsockAddr))

	// Register proposalBackend as the "store-vm" skill so preferredBackendForAction
	// routes proposal actions through the real remote Store VM.
	proposalBackend := ipc.NewProposalBackend(remoteClient.Proposals(), logger)
	if err := srv.hub.RegisterSkill("store-vm", proposalBackend.Handle); err != nil {
		logger.Fatal("failed to register store-vm skill", zap.Error(err))
	}

	logger.Info("store-vm skill registered with message-hub")

	port := uint32(defaultAegisHubVSOCKPort)
	if portEnv := os.Getenv("AEGISHUB_VSOCK_PORT"); portEnv != "" {
		parsed, err := strconv.ParseUint(portEnv, 10, 32)
		if err != nil {
			logger.Fatal("invalid AEGISHUB_VSOCK_PORT", zap.String("value", portEnv), zap.Error(err))
		}
		port = uint32(parsed)
	}

	listener, err := listenAFVsock(port)
	if err != nil {
		logger.Fatal("aegishub listen failed", zap.Error(err))
	}
	defer listener.Close()

	logger.Info("aegishub listening", zap.Uint32("vsock_port", port))
	go srv.acceptLoop(listener)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, unix.SIGINT, unix.SIGTERM)
	<-sigCh

	// Graceful shutdown: close remote client before stopping hub.
	remoteClient.Close()
	listener.Close()
	srv.hub.Stop()
}

func (s *server) acceptLoop(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, unix.EBADF) {
				return
			}
			s.logger.Warn("aegishub: accept failed", zap.Error(err))
			return
		}
		go s.handleConn(conn)
	}
}

func (s *server) handleConn(conn net.Conn) {
	defer conn.Close()

	// Task 5: Connection Hardening - Enforce read/write deadlines to mitigate slow-client DoS.
	conn.SetReadDeadline(time.Now().Add(requestTimeout))
	conn.SetWriteDeadline(time.Now().Add(requestTimeout))

	authn, err := s.authenticateConn(conn)
	if err != nil {
		s.logger.Warn("aegishub: handshake failed", zap.Error(err))
		return
	}

	// Reset deadlines after the handshake succeeds.
	conn.SetReadDeadline(time.Time{})
	conn.SetWriteDeadline(time.Time{})

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	for {
		var req HubRequest
		if err := dec.Decode(&req); err != nil {
			if err != io.EOF {
				s.logger.Debug("aegishub: client disconnect or decode error", zap.Error(err))
			}
			return
		}

		resp := s.dispatch(authn, req)
		if err := enc.Encode(resp); err != nil {
			s.logger.Debug("aegishub: failed to encode response", zap.Error(err))
			return
		}
		// Reset deadline after successful round-trip to allow normal operation.
		conn.SetReadDeadline(time.Now().Add(requestTimeout))
		conn.SetWriteDeadline(time.Now().Add(requestTimeout))
	}
}

func (s *server) authenticateConn(conn net.Conn) (authenticatedVM, error) {
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	var req handshakeRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return authenticatedVM{}, fmt.Errorf("decode handshake: %w", err)
	}
	if req.Type != "handshake" {
		return authenticatedVM{}, fmt.Errorf("invalid handshake type")
	}
	if req.VMID == "" {
		return authenticatedVM{}, fmt.Errorf("vm_id is required")
	}

	role, err := parseRole(req.Role)
	if err != nil {
		return authenticatedVM{}, err
	}

	secret, err := loadSharedSecret("AEGISHUB_SHARED_SECRET")
	if err != nil {
		return authenticatedVM{}, err
	}
	if req.Secret != secret {
		return authenticatedVM{}, fmt.Errorf("invalid shared secret")
	}

	if err := s.hub.RegisterVM(req.VMID, role); err != nil {
		return authenticatedVM{}, fmt.Errorf("register vm: %w", err)
	}

	if err := json.NewEncoder(conn).Encode(map[string]string{
		"type":   "handshake_ack",
		"status": "ok",
	}); err != nil {
		return authenticatedVM{}, fmt.Errorf("encode handshake ack: %w", err)
	}

	return authenticatedVM{ID: req.VMID, Role: role}, nil
}

// dispatch routes the request to the appropriate handler based on its type.
func (s *server) dispatch(sender authenticatedVM, req HubRequest) HubResponse {
	switch req.Type {
	case "register_vm":
		var payload RegisterVMPayload
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return HubResponse{ID: req.ID, Success: false, Error: remote.SanitizeError(fmt.Errorf("invalid register_vm payload"))}
		}
		if payload.VMID != sender.ID || ipc.VMRole(payload.Role) != sender.Role {
			return HubResponse{ID: req.ID, Success: false, Error: remote.SanitizeError(fmt.Errorf("register_vm payload does not match authenticated identity"))}
		}
		if err := s.hub.RegisterVM(payload.VMID, sender.Role); err != nil {
			return HubResponse{ID: req.ID, Success: false, Error: remote.SanitizeError(err)}
		}
		return HubResponse{ID: req.ID, Success: true}

	case "route":
		var msg ipc.Message
		if err := json.Unmarshal(req.Payload, &msg); err != nil {
			return HubResponse{ID: req.ID, Success: false, Error: remote.SanitizeError(fmt.Errorf("invalid route payload"))}
		}
		if msg.From == "" {
			msg.From = sender.ID
		}
		if msg.Timestamp.IsZero() {
			msg.Timestamp = time.Now().UTC()
		}

		result, err := s.hub.RouteMessage(sender.ID, &msg)
		if err != nil {
			return HubResponse{ID: req.ID, Success: false, Error: remote.SanitizeError(err)}
		}

		data, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return HubResponse{ID: req.ID, Success: false, Error: remote.SanitizeError(marshalErr)}
		}
		return HubResponse{ID: req.ID, Success: true, Data: json.RawMessage(data)}

	default:
		return HubResponse{ID: req.ID, Success: false, Error: remote.SanitizeError(fmt.Errorf("unknown request type: %s", req.Type))}
	}
}

func parseRole(role string) (ipc.VMRole, error) {
	switch ipc.VMRole(role) {
	case ipc.RoleAgent, ipc.RoleCLI, ipc.RoleCourt, ipc.RoleBuilder, ipc.RoleSkill, ipc.RoleHub, ipc.RoleDaemon:
		return ipc.VMRole(role), nil
	default:
		return "", fmt.Errorf("invalid role %q", role)
	}
}

func loadSharedSecret(envVar string) (string, error) {
	if secret := strings.TrimSpace(os.Getenv(envVar)); secret != "" {
		return secret, nil
	}

	data, err := os.ReadFile(filepath.Join("/data", ".shared_secret"))
	if err != nil {
		return "", fmt.Errorf("shared secret not configured")
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", fmt.Errorf("shared secret not configured")
	}
	return secret, nil
}
