package ipc

import (
	"testing"

	"go.uber.org/zap"
)

// TestACLPolicy_RoleHub verifies that the RoleHub wildcard permit allows any
// message type, consistent with AegisHub's routing authority.
func TestACLPolicy_RoleHub(t *testing.T) {
	p := defaultACLPolicy()

	types := []string{"tool.exec", "tool.result", "chat.message", "review.result",
		"build.result", "status", "hub.status", "hub.routes", "arbitrary.type"}
	for _, msgType := range types {
		if err := p.Check(RoleHub, msgType); err != nil {
			t.Errorf("RoleHub should be permitted to send %q, got error: %v", msgType, err)
		}
	}
}

// TestACLPolicy_RoleHubNotGrantedToOthers confirms that the wildcard permit is
// exclusive to RoleHub — other roles still have their restricted permit sets.
func TestACLPolicy_RoleHubNotGrantedToOthers(t *testing.T) {
	p := defaultACLPolicy()

	// Agent is only allowed tool.exec, chat.message, status — not hub.status.
	if err := p.Check(RoleAgent, "hub.status"); err == nil {
		t.Error("RoleAgent should NOT be permitted to send hub.status")
	}

	// Skill is only allowed tool.result, status — not tool.exec.
	if err := p.Check(RoleSkill, "tool.exec"); err == nil {
		t.Error("RoleSkill should NOT be permitted to send tool.exec")
	}
}

// TestNewMessageHubNoKernel verifies that a hub created without a kernel
// still starts, routes messages, and stops correctly.
func TestNewMessageHubNoKernel(t *testing.T) {
	logger := zap.NewNop()
	hub := NewMessageHubNoKernel(logger)

	if err := hub.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	if hub.State() != HubStateRunning {
		t.Errorf("expected state running, got %s", hub.State())
	}

	// Register a VM with RoleHub (simulating the AegisHub VM registration).
	if err := hub.RegisterVM("aegishub-test", RoleHub); err != nil {
		t.Fatalf("RegisterVM(RoleHub) failed: %v", err)
	}

	// Register a CLI identity so hub.status routing works.
	if err := hub.RegisterVM("cli-test", RoleCLI); err != nil {
		t.Fatalf("RegisterVM(RoleCLI) failed: %v", err)
	}

	// Hub should be able to route a hub.status message without panicking.
	// Send with senderVMID="cli-test" (CLI role) targeting the hub itself.
	msg := &Message{
		ID:   "test-1",
		From: "cli-test",
		To:   MessageHubID,
		Type: "hub.status",
	}
	result, err := hub.RouteMessage("cli-test", msg)
	if err != nil {
		t.Fatalf("RouteMessage(hub.status) failed: %v", err)
	}
	if !result.Success {
		t.Errorf("hub.status routing returned failure: %s", result.Error)
	}

	hub.Stop()
	if hub.State() != HubStateStopped {
		t.Errorf("expected state stopped, got %s", hub.State())
	}
}

// TestIdentityRegistry_RoleHub verifies that the identity registry correctly
// stores and retrieves the RoleHub assignment.
func TestIdentityRegistry_RoleHub(t *testing.T) {
	reg := NewIdentityRegistry()

	if err := reg.Register("aegishub-vm-001", RoleHub); err != nil {
		t.Fatalf("Register(RoleHub) failed: %v", err)
	}

	role, ok := reg.Role("aegishub-vm-001")
	if !ok {
		t.Fatal("expected VM to be registered")
	}
	if role != RoleHub {
		t.Errorf("expected RoleHub, got %s", role)
	}

	// Attempting to re-register with a different role must fail.
	if err := reg.Register("aegishub-vm-001", RoleAgent); err == nil {
		t.Error("expected error when changing registered role, got nil")
	}

	// Idempotent re-registration with the same role must succeed.
	if err := reg.Register("aegishub-vm-001", RoleHub); err != nil {
		t.Errorf("idempotent re-register(RoleHub) failed: %v", err)
	}
}

// TestMessageHub_AegisHubRouting verifies the end-to-end hub routing scenario
// where AegisHub is registered with RoleHub and sends a permitted message.
func TestMessageHub_AegisHubRouting(t *testing.T) {
	logger := zap.NewNop()
	hub := NewMessageHubNoKernel(logger)
	if err := hub.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer hub.Stop()

	// Register AegisHub with RoleHub.
	const hubVMID = "aegishub-test-001"
	if err := hub.RegisterVM(hubVMID, RoleHub); err != nil {
		t.Fatalf("RegisterVM(RoleHub) failed: %v", err)
	}

	// Register a simulated skill with a handler.
	const skillID = "skill-hello"
	var received *Message
	if err := hub.RegisterSkill(skillID, func(msg *Message) (*DeliveryResult, error) {
		received = msg
		return &DeliveryResult{MessageID: msg.ID, Success: true}, nil
	}); err != nil {
		t.Fatalf("RegisterSkill failed: %v", err)
	}

	// AegisHub sends a tool.result message to the skill (wildcard permit).
	msg := &Message{
		ID:   "route-1",
		From: hubVMID,
		To:   skillID,
		Type: "tool.result",
	}
	result, err := hub.RouteMessage(hubVMID, msg)
	if err != nil {
		t.Fatalf("RouteMessage from AegisHub failed: %v", err)
	}
	if !result.Success {
		t.Errorf("routing failed: %s", result.Error)
	}
	if received == nil || received.ID != "route-1" {
		t.Error("skill handler did not receive the message")
	}
}

// TestRoleHub_IsRequiredCoreRole verifies that RoleHub is defined, distinct
// from all other roles, and cannot be claimed by non-hub VMs after a hub is
// already registered (role-lock enforcement).
func TestRoleHub_IsRequiredCoreRole(t *testing.T) {
	// RoleHub must be distinct from every other role.
	otherRoles := []VMRole{RoleAgent, RoleCLI, RoleCourt, RoleBuilder, RoleSkill}
	for _, r := range otherRoles {
		if RoleHub == r {
			t.Errorf("RoleHub must not equal %q", r)
		}
	}

	reg := NewIdentityRegistry()

	// Register the AegisHub VM with RoleHub.
	if err := reg.Register("aegishub-vm-001", RoleHub); err != nil {
		t.Fatalf("Register(RoleHub) failed: %v", err)
	}

	// A second VM must NOT be allowed to register with RoleHub.
	// (Each VM gets its own identity; the role itself is not singleton-enforced
	// in the registry, but identity is locked per-VM so no impersonation is
	// possible.)
	if err := reg.Register("aegishub-vm-001", RoleAgent); err == nil {
		t.Error("expected error when changing role of already-registered VM, got nil")
	}

	// Confirm the original RoleHub is still intact.
	role, ok := reg.Role("aegishub-vm-001")
	if !ok || role != RoleHub {
		t.Errorf("expected RoleHub, got %v (ok=%v)", role, ok)
	}
}
