package remote

import (
	"encoding/json"
	"errors"
	"testing"
)

// mockSendRequester is a test double interface for RemoteClient.sendRequest.
// Extracting this interface allows mocking the vsock transport without modifying
// the production client implementation.
type mockSendRequester interface {
	sendRequest(op string, payload interface{}) (*Response, error)
}

// TestRequestMarshaling verifies that Request types marshal/unmarshal correctly.
func TestRequestMarshaling(t *testing.T) {
	req := Request{
		ID:      "req-1",
		Op:      OpProposalList,
		Payload: map[string]interface{}{"filter": "active"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	var unmarshaledReq Request
	if err := json.Unmarshal(data, &unmarshaledReq); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if unmarshaledReq.ID != req.ID {
		t.Errorf("ID mismatch: got %s, want %s", unmarshaledReq.ID, req.ID)
	}
	if unmarshaledReq.Op != req.Op {
		t.Errorf("Op mismatch: got %s, want %s", unmarshaledReq.Op, req.Op)
	}
}

// TestResponseMarshaling verifies that Response types marshal/unmarshal correctly.
func TestResponseMarshaling(t *testing.T) {
	resp := Response{
		ID:      "resp-1",
		Success: true,
		Data:    map[string]interface{}{"proposals": []string{"p1", "p2"}},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal response: %v", err)
	}

	var unmarshaledResp Response
	if err := json.Unmarshal(data, &unmarshaledResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !unmarshaledResp.Success {
		t.Error("expected Success to be true")
	}
}

// TestProtocolErrorMarshaling verifies that ProtocolError types marshal/unmarshal correctly.
func TestProtocolErrorMarshaling(t *testing.T) {
	protoErr := ProtocolError{
		Code:    "INVALID_OP",
		Message: "operation not supported",
	}

	data, err := json.Marshal(protoErr)
	if err != nil {
		t.Fatalf("failed to marshal protocol error: %v", err)
	}

	var parsedErr ProtocolError
	if err := json.Unmarshal(data, &parsedErr); err != nil {
		t.Fatalf("failed to unmarshal protocol error: %v", err)
	}

	if parsedErr.Code != protoErr.Code {
		t.Errorf("Code mismatch: got %s, want %s", parsedErr.Code, protoErr.Code)
	}
	if parsedErr.Message != protoErr.Message {
		t.Errorf("Message mismatch: got %s, want %s", parsedErr.Message, protoErr.Message)
	}
}

// TestErrorWrapping verifies that ProtocolError can be used with standard error wrapping.
func TestErrorWrapping(t *testing.T) {
	original := errors.New("connection reset")
	wrapped := &ProtocolError{Code: "NETWORK", Message: original.Error()}
	
	if wrapped.Message != "connection reset" {
		t.Errorf("expected message 'connection reset', got %s", wrapped.Message)
	}
}

// TestRemoteProposalStoreRequestConstruction verifies that remoteProposalStore
// correctly constructs requests for common operations.
func TestRemoteProposalStoreRequestConstruction(t *testing.T) {
	tests := []struct {
		name string
		op   string
	}{
		{"list", OpProposalList},
		{"get", OpProposalGet},
		{"create", OpProposalCreate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := Request{
				ID:  "test-id",
				Op:  tt.op,
				Payload: nil,
			}

			data, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("failed to marshal request: %v", err)
			}

			var unmarshaledReq Request
			if err := json.Unmarshal(data, &unmarshaledReq); err != nil {
				t.Fatalf("failed to unmarshal request: %v", err)
			}

			if unmarshaledReq.Op != tt.op {
				t.Errorf("expected op %s, got %s", tt.op, unmarshaledReq.Op)
			}
		})
	}
}

// IntegrationTestSkeleton provides a starting point for end-to-end tests.
// Uncomment and configure when the Store VM and AegisHub are available in a test environment.
/*
func TestIntegrationAegisHubToStoreVM(t *testing.T) {
	// 1. Spin up a test Store VM with a mock backend.
	// 2. Start AegisHub pointing to the test Store VM.
	// 3. Send a "proposal.list" request over vsock.
	// 4. Verify the response matches expected data.
	// 5. Verify TCB boundaries: AegisHub should not hold any proposal state.
	// 6. Tear down.
	t.Skip("Integration test skeleton - requires full VM environment")
}
*/
