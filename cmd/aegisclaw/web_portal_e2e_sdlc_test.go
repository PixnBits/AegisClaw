// Web Portal E2E Test for full autonomous SDLC flow — Issue #35
// This test starts the real AegisClaw system (including web portal) and drives everything
// exclusively through the UI/API surface. It detects disconnects like "Court approved but no code generated".

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/testutil"
	"github.com/stretchr/testify/require"
)

// sdlcStatus is the portal-reported status of a proposal going through the full SDLC.
type sdlcStatus struct {
	Phase         string `json:"phase"`
	CourtApproved bool   `json:"court_approved"`
	CodeGenerated bool   `json:"code_generated"`
	PRURL         string `json:"pr_url"`
	Deployed      bool   `json:"deployed"`
	Error         string `json:"error"`
}

// extractProposalID reads a proposal ID from an HTTP response body.
func extractProposalID(resp *http.Response) string {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var v struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &v) //nolint:errcheck
	return v.ID
}

// getPortalSDLCStatus polls the portal for the current SDLC status of a proposal.
func getPortalSDLCStatus(ctx context.Context, baseURL, proposalID string) (sdlcStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/proposals/%s/status", baseURL, proposalID), nil)
	if err != nil {
		return sdlcStatus{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return sdlcStatus{}, err
	}
	defer resp.Body.Close()
	var s sdlcStatus
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return sdlcStatus{}, err
	}
	return s, nil
}

// getPortalAuditLog fetches the audit log for a proposal from the portal.
func getPortalAuditLog(ctx context.Context, baseURL, proposalID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/proposals/%s/audit", baseURL, proposalID), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

func TestWebPortalFullSDLC_Autonomous(t *testing.T) {
	// This test requires a live daemon + web portal. Skip in CI unless explicitly enabled.
	if testing.Short() {
		t.Skip("skipping live portal E2E test in short mode")
	}
	if os.Getenv("AEGIS_RUN_PORTAL_E2E") != "1" {
		t.Skip("skipping live portal E2E test (set AEGIS_RUN_PORTAL_E2E=1 to enable)")
	}

	ctx := testutil.NewTestContext(t)
	defer ctx.Cleanup()

	// Start full system with portal (real daemon, no shortcuts)
	srv := testutil.StartAegisClawWithPortal(ctx)
	defer func() { _ = srv }()

	// 1. Submit proposal exactly as a user would via portal
	propPayload := `{"title":"Autonomous Vision Skill","description":"Secure multimodal perception skill","permissions":["read-image","read-audio"]}`
	resp, err := http.Post(srv.URL+"/api/proposals", "application/json", strings.NewReader(propPayload))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	proposalID := extractProposalID(resp)

	// 2. Poll portal status — this is the key: we observe what the *system* does
	bgCtx := context.Background()
	var status sdlcStatus
	for i := 0; i < 60; i++ { // generous timeout for real builder/court
		status, err = getPortalSDLCStatus(bgCtx, srv.URL, proposalID)
		require.NoError(t, err)

		t.Logf("SDLC Phase: %s | CodeGen: %v | Court: %v | Deployed: %v",
			status.Phase, status.CodeGenerated, status.CourtApproved, status.Deployed)

		if status.Phase == "completed" || status.Error != "" {
			break
		}
		time.Sleep(2 * time.Second)
	}

	// Critical regression detectors
	require.True(t, status.CourtApproved, "Court must approve")
	require.True(t, status.CodeGenerated, "Code MUST be generated after approval (detects disconnect)")
	require.NotEmpty(t, status.PRURL, "PR must be created")
	require.True(t, status.Deployed, "Skill must reach deployment")

	// Audit trail visibility
	audit, err := getPortalAuditLog(bgCtx, srv.URL, proposalID)
	require.NoError(t, err)
	require.Contains(t, audit, "builder.pipeline.completed")
	require.Contains(t, audit, "court.code_review.passed")
	require.Contains(t, audit, "deployment.success")
}
