package remote

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"testing"
)

// TestHandshakeInvalidSecret verifies that the handshake rejects invalid credentials.
func TestHandshakeInvalidSecret(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer serverConn.Close()

		var req map[string]string
		if err := json.NewDecoder(serverConn).Decode(&req); err != nil {
			t.Errorf("server decode handshake: %v", err)
			return
		}
		if req["type"] != "handshake" {
			t.Errorf("unexpected handshake type %q", req["type"])
		}
		if err := json.NewEncoder(serverConn).Encode(map[string]string{
			"type":   "handshake_ack",
			"status": "denied",
		}); err != nil {
			t.Errorf("server encode handshake ack: %v", err)
		}
	}()

	err := performHandshake(clientConn, "wrong-secret")
	if err == nil {
		t.Fatal("performHandshake() error = nil, want invalid response error")
	}
	<-done
}

func TestSendRequestReturnsRawJSON(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer serverConn.Close()

		var req Request
		if err := json.NewDecoder(serverConn).Decode(&req); err != nil {
			t.Errorf("server decode request: %v", err)
			return
		}
		if req.Op != "proposal.get" {
			t.Errorf("unexpected op %q", req.Op)
		}
		if string(req.Payload) != `{"id":"abc123"}` {
			t.Errorf("unexpected payload %s", req.Payload)
		}

		if err := json.NewEncoder(serverConn).Encode(Response{
			ID:      req.ID,
			Success: true,
			Data:    json.RawMessage(`{"id":"abc123","title":"demo"}`),
		}); err != nil {
			t.Errorf("server encode response: %v", err)
		}
	}()

	client := &RemoteClient{
		conn:   clientConn,
		reader: bufio.NewReader(clientConn),
	}
	got, err := client.sendRequest("proposal.get", map[string]string{"id": "abc123"})
	if err != nil {
		t.Fatalf("sendRequest() error = %v", err)
	}
	if string(got) != `{"id":"abc123","title":"demo"}` {
		t.Fatalf("sendRequest() = %s, want raw JSON payload", got)
	}
	<-done
}

func TestParseVsockAddr(t *testing.T) {
	tests := []struct {
		addr     string
		wantCID  uint32
		wantPort uint32
		wantErr  bool
	}{
		{"vsock://1:9999", 1, 9999, false},
		{"vsock://0:0", 0, 0, false},
		{"invalid", 0, 0, true},
		{"vsock://abc:1", 0, 0, true},
	}

	for _, tt := range tests {
		cid, port, err := parseVsockAddr(tt.addr)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseVsockAddr(%q) error = %v, wantErr %v", tt.addr, err, tt.wantErr)
			continue
		}
		if !tt.wantErr {
			if cid != tt.wantCID || port != tt.wantPort {
				t.Errorf("parseVsockAddr(%q) = %d:%d, want %d:%d", tt.addr, cid, port, tt.wantCID, tt.wantPort)
			}
		}
	}
}

func TestSanitizeError(t *testing.T) {
	if got := SanitizeError(nil); got != "" {
		t.Errorf("SanitizeError(nil) = %q, want empty", got)
	}
	if got := SanitizeError(errors.New("test")); got != "internal error" {
		t.Errorf("SanitizeError(err) = %q, want internal error", got)
	}
}
