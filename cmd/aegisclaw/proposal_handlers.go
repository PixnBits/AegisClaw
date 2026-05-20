package main

import (
	"context"
	"encoding/json"

	"github.com/PixnBits/AegisClaw/internal/api"
)

// Proposal handlers (Phase 8): lightweight skeletons that forward through
// ControlPlaneProxy to AegisHub. Real proposal store logic lives in the
// Store VM. These handlers are not yet registered on the API socket;
// they serve as the pattern for future wiring (e.g. "proposal.list",
// "proposal.status").
//
// TODO(Phase 9): Register proposal handlers on the API socket and connect
// to a real Store VM backend (e.g. via RegisterSkill("store-vm", ...))
// so delegation in handleControlPlaneRequest returns live data instead of samples.

func makeProposalListHandler(proxy *ControlPlaneProxy) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if proxy == nil {
			return &api.Response{Error: "control plane proxy not available"}
		}
		resp, err := proxy.Forward(ctx, ControlPlaneRequest{
			Action: "proposal.list",
			Data:   data,
		})
		if err != nil || !resp.Success {
			return &api.Response{Error: "proposal.list via AegisHub failed"}
		}
		return &api.Response{Success: true, Data: resp.Data}
	}
}

func makeProposalStatusHandler(proxy *ControlPlaneProxy) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		if proxy == nil {
			return &api.Response{Error: "control plane proxy not available"}
		}
		resp, err := proxy.Forward(ctx, ControlPlaneRequest{
			Action: "proposal.status",
			Data:   data,
		})
		if err != nil || !resp.Success {
			return &api.Response{Error: "proposal.status via AegisHub failed"}
		}
		return &api.Response{Success: true, Data: resp.Data}
	}
}