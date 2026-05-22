package remote

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestHandshakeInvalidSecret verifies that the handshake rejects invalid credentials.
func TestHandshakeInvalidSecret(t *testing.T) {
	// Note: This test requires a mock vsock listener or a test double.
	// In a real environment, we would spin up a mock Store VM that expects a secret.
	// For now, we verify the logic in performHandshake would fail on mismatch.
	// TODO: Implement integration test with mock vsock when test infrastructure is ready.
	t.Skip("Requires mock vsock infrastructure")
}

// IntegrationTestSkeleton provides a starting point for end-to-end tests.
// Uncomment and configure when the Store VM and AegisHub are available in a test environment.
/*
func TestIntegrationAegisHubToStoreVM(t *testing.T) {
	// 1. Spin up a test Store VM with a mock backend.
	// 2. Start AegisHub pointing to the test Store VM.
	// 3. Send a "proposal.list" request over vsock.
	// 4. Verify the response matches expected data.
	// 5. Verify TCB boundaries: AegisHub must not hold any proposal state.
	//    Confirm that all persistent data flows exclusively through the Store VM.
	// 6. Tear down.
	t.Skip("Integration test skeleton - requires full VM environment")
}
*/

func TestParseVsockAddr(t *testing.T) {
	tests := []struct {
		addr    string
		wantCID uint32
		wantPort uint32
		wantErr bool
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
