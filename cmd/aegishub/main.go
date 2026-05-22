// Package main implements AegisHub, the system IPC router microVM for AegisClaw.
//
// Security Summary:
// - AegisHub acts as a strict mediator. It holds no persistent proposal state.
// - All vsock connections are authenticated via mutual handshake.
// - Input is strictly validated; errors are sanitized to prevent information leakage.
// - Connection timeouts prevent slow-client DoS.
// Trust Boundary: AegisHub trusts the Store VM only after successful handshake.
// All external messages are considered hostile until proven otherwise.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PixnBits/AegisClaw/internal/ipc"
	"github.com/PixnBits/AegisClaw/internal/store/remote"
	"go.uber.org/zap"
)

// ... existing code ...

func (s *server) handleConn(conn net.Conn) {
	defer conn.Close()
	// Task 5: Connection Hardening - Enforce read/write deadlines to mitigate slow-client DoS.
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	conn.SetWriteDeadline(time.Now().Add(30 * time.Second))

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

		resp := s.dispatch(req)
		if err := enc.Encode(resp); err != nil {
			s.logger.Debug("aegishub: failed to encode response", zap.Error(err))
			return
		}
		// Reset deadline after successful round-trip to allow normal operation.
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	}
}

// sanitizeError returns a generic error message for external consumption.
func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	return "internal error"
}

// dispatch routes the request to the appropriate handler based on its type.
func (s *server) dispatch(req HubRequest) HubResponse {
	switch req.Type {
	case "register_vm":
		var payload RegisterVMPayload
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return HubResponse{ID: req.ID, Success: false, Error: remote.SanitizeError(fmt.Errorf("invalid register_vm payload"))}
		}
		// TODO: Implement VM registration logic if needed by the hub.
		return HubResponse{ID: req.ID, Success: true}

	case "route":
		var payload RoutePayload
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return HubResponse{ID: req.ID, Success: false, Error: remote.SanitizeError(fmt.Errorf("invalid route payload"))}
		}
		// TODO: Implement cross-VM routing logic.
		return HubResponse{ID: req.ID, Success: true, Data: json.RawMessage(`{"routed": true}`)}

	default:
		// Delegate to registered skills (e.g., store-vm)
		result, err := s.hub.Dispatch(req.Type, req.Payload)
		if err != nil {
			return HubResponse{ID: req.ID, Success: false, Error: remote.SanitizeError(err)}
		}
		data, _ := json.Marshal(result)
		return HubResponse{ID: req.ID, Success: true, Data: json.RawMessage(data)}
	}
}

// ... existing code ...
