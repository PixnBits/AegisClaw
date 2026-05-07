package testutil

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/court"
	// add other imports as needed
)

// StartAegisClawWithPortal starts the full daemon + web portal for E2E tests
type TestServer struct {
	URL string
	// shutdown func
}

func StartAegisClawWithPortal(ctx context.Context) *TestServer {
	// TODO: implement full startup (daemon + HTTP server)
	// For now stub
	srv := httptest.NewServer(nil)
	return &TestServer{URL: srv.URL}
}

// Placeholder helpers for web portal E2E
func extractProposalID(resp *http.Response) string { return "stub-id" }
func getPortalSDLCStatus(ctx context.Context, baseURL, proposalID string) (SDLCStatus, error) {
	return SDLCStatus{}, nil
}
func getPortalAuditLog(ctx context.Context, baseURL, proposalID string) (string, error) {
	return "", nil
}

type SDLCStatus struct {
	Phase         string
	CourtApproved bool
	CodeGenerated bool
	PRURL         string
	Deployed      bool
	Error         string
}
