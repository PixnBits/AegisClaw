package main

import (
	"strings"
	"testing"
)

// TestCLI_TDDGapManifest lists daemon actions still marked stub in the contract.
// When implementing a feature, change its entry in daemonEndpointContract from
// implStub to implReady and implement the handler; TestDaemonAPI_EndpointContract
// will then require Success.
func TestCLI_TDDGapManifest(t *testing.T) {
	var stubs []string
	for _, tc := range daemonEndpointContract {
		if tc.impl == implStub {
			stubs = append(stubs, tc.action)
		}
	}
	// Note: startOnlyDaemonContract loop removed (Phase 9 test cleanup).
	// court.vote and related Court handlers removed from Host Daemon TCB.
	if len(stubs) == 0 {
		t.Log("no stub endpoints — all contract actions are ready")
		return
	}
	t.Logf("TDD backlog (%d stub endpoints): %s", len(stubs), strings.Join(stubs, ", "))
	// This test always passes; it documents gaps in CI output. Contract tests
	// fail if a stub returns Success (false positive) or unknown action (unregistered).
}

// TestCLI_StubEndpointsNeverReturnSuccess guards against silent stub regressions.
func TestCLI_StubEndpointsNeverReturnSuccess(t *testing.T) {
	srv, env := newContractAPIServer(t)
	ctx := t.Context()

	for _, tc := range daemonEndpointContract {
		if tc.impl != implStub {
			continue
		}
		tc := tc
		t.Run(tc.action, func(t *testing.T) {
			resp := srv.CallDirect(ctx, tc.action, tc.payload(t, env))
			if resp == nil {
				t.Fatal("nil response")
			}
			if resp.Success {
				t.Fatalf("%s: stub endpoint returned Success — update contract to implReady or fix handler", tc.action)
			}
		})
	}
}
