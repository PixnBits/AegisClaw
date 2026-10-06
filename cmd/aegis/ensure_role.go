package main

import (
	"context"

	"github.com/sirupsen/logrus"

	"AegisClaw/internal/runtime"
)

// ensureRoler starts an on-demand role VM. *runtime.Orchestrator implements it.
type ensureRoler interface {
	EnsureRoleAgent(ctx context.Context, roleType string, channelHint string) (string, error)
}

// handleEnsureRole is the ensure_role decision shared by the control socket
// and the daemon-orchestrator hub receiver.
//
// A reserved id, a role that is not on the allowlist, or an id that fails
// runtime.ValidateVMID returns before EnsureRoleAgent and startBridge.
// Callers must also skip channel membership on that error
// (ensureRoleAddsChannelMember). The paired path (role "agent" or "") is not
// a composed id; its agent-<session> and memory-<session> ids are still
// validated.
func handleEnsureRole(orch ensureRoler, role, channel string, startBridge func(string)) (string, error) {
	if id, err := runtime.CheckRoleAgentID(role, channel); err != nil {
		logrus.Warnf("Audit: rejected ensure_role id %q (role=%q channel=%q): %v", id, role, channel, err)
		return "", err
	}
	id, err := orch.EnsureRoleAgent(context.Background(), role, channel)
	if err != nil {
		if runtime.EnsureRoleRefused(err) {
			logrus.Warnf("Audit: rejected ensure_role (role=%q channel=%q): %v", role, channel, err)
		}
		return "", err
	}
	if startBridge != nil {
		startBridge(id)
	}
	return id, nil
}

// ensureRoleAddsChannelMember reports whether a channel membership may be
// recorded for this ensure_role result. A refusal (reserved, unknown role,
// or invalid id) must not add the member. Other errors still do, matching
// the hub receiver's previous behavior of attaching the role even when the
// VM start failed.
func ensureRoleAddsChannelMember(err error) bool {
	return !runtime.EnsureRoleRefused(err)
}
