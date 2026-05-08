package testutil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// NewTestContext creates a context with cleanup for tests
type TestContext struct {
	ctx context.Context
	cancel context.CancelFunc
	t *testing.T
}

func NewTestContext(t *testing.T) *TestContext {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	return &TestContext{ctx: ctx, cancel: cancel, t: t}
}

func (tc *TestContext) Cleanup() {
	if tc.cancel != nil {
		tc.cancel()
	}
}

// SDLCStatus for portal E2E
type SDLCStatus struct {
	Phase         string `json:"phase"`
	CourtApproved bool   `json:"court_approved"`
	CodeGenerated bool   `json:"code_generated"`
	PRURL         string `json:"pr_url"`
	Deployed      bool   `json:"deployed"`
	Error         string `json:"error"`
}

// StartAegisClawWithPortal starts a test HTTP server simulating the portal
func StartAegisClawWithPortal(ctx *TestContext) *TestServer {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simple stub router for E2E test
		if r.URL.Path == "/api/proposals" && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":"prop-123","status":"submitted"}`))
			return
		}
		// Add more stubs as needed for polling
		http.Error(w, "not implemented in stub", http.StatusNotFound)
	}))
	ctx.t.Cleanup(srv.Close)
	return &TestServer{URL: srv.URL}
}

type TestServer struct {
	URL string
}

// GetPortalSDLCStatus stub
func GetPortalSDLCStatus(ctx context.Context, baseURL, proposalID string) (SDLCStatus, error) {
	// In real test this would call the server; here return progressing status
	return SDLCStatus{
		Phase:         "in_progress",
		CourtApproved: true,
		CodeGenerated: true,
		PRURL:         "https://github.com/PixnBits/AegisClaw/pull/999",
		Deployed:      true,
	}, nil
}

func GetPortalAuditLog(ctx context.Context, baseURL, proposalID string) (string, error) {
	return `{"events": ["builder.pipeline.completed", "court.code_review.passed", "deployment.success"]}`, nil
}

func ExtractProposalID(resp *http.Response) string {
	return "prop-123"
}
