// Web Portal E2E Test for full autonomous SDLC flow — Issue #35
// This test starts the real AegisClaw system (including web portal) and drives everything
// exclusively through the UI/API surface. It detects disconnects like "Court approved but no code generated".

package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestWebPortalFullSDLC_Autonomous(t *testing.T) {
	ctx := testutil.NewTestContext(t)
	defer ctx.Cleanup()

	// Start full system with portal (real daemon, no shortcuts)
	srv := testutil.StartAegisClawWithPortal(ctx) // assumes this helper exists or is easy to add
	defer srv.Shutdown(ctx)

	// 1. Submit proposal exactly as a user would via portal
	propPayload := `{"title":"Autonomous Vision Skill","description":"Secure multimodal perception skill","permissions":["read-image","read-audio"]}`
	resp, err := http.Post(srv.URL+"/api/proposals", "application/json", strings.NewReader(propPayload))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	proposalID := extractProposalID(resp) // helper to parse JSON

	// 2. Poll portal status — this is the key: we observe what the *system* does
	var status SDLCStatus
	for i := 0; i < 60; i++ { // generous timeout for real builder/court
		status, err = getPortalSDLCStatus(ctx, srv.URL, proposalID)
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
	audit, err := getPortalAuditLog(ctx, srv.URL, proposalID)
	require.NoError(t, err)
	require.Contains(t, audit, "builder.pipeline.completed")
	require.Contains(t, audit, "court.code_review.passed")
	require.Contains(t, audit, "deployment.success")
}

// TODO: Add these helpers to internal/testutil if missing:
// - StartAegisClawWithPortal
// - getPortalSDLCStatus, getPortalAuditLog, extractProposalID
// - type SDLCStatus struct { Phase string; CourtApproved, CodeGenerated, Deployed bool; PRURL, Error string }