package main

import (
	"path/filepath"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"go.uber.org/zap"
)

// testProposalStore creates a temporary proposal store for use in tests.
func testProposalStore(t *testing.T) *proposal.Store {
	t.Helper()
	dir := t.TempDir()
	logger, _ := zap.NewDevelopment()
	store, err := proposal.NewStore(filepath.Join(dir, "proposals.json"), logger)
	if err != nil {
		t.Fatalf("failed to create proposal store: %v", err)
	}
	return store
}

// makeApprovedProposal is a helper that creates and imports a proposal in
// approved state for use in capability enforcement tests.
func makeApprovedProposal(t *testing.T, store *proposal.Store, skillName string, caps *proposal.SkillCapabilities, np *proposal.ProposalNetworkPolicy) {
	t.Helper()
	p, err := proposal.NewProposal("test skill "+skillName, "test description", proposal.CategoryNewSkill, "tester")
	if err != nil {
		t.Fatalf("NewProposal: %v", err)
	}
	p.TargetSkill = skillName
	p.Status = proposal.StatusApproved
	p.Capabilities = caps
	p.NetworkPolicy = np
	if err := store.Import(p); err != nil {
		t.Fatalf("Import: %v", err)
	}
}

// TestSkillNetworkPolicy_NilStore verifies that a nil proposal store causes
// the policy to fall back to the safest option (no network).
func TestSkillNetworkPolicy_NilStore(t *testing.T) {
	env := &runtimeEnv{ProposalStore: nil}
	pol := skillNetworkPolicy("my-skill", env)
	if !pol.NoNetwork {
		t.Error("expected NoNetwork=true when ProposalStore is nil")
	}
	if !pol.DefaultDeny {
		t.Error("expected DefaultDeny=true when ProposalStore is nil")
	}
}

// TestSkillNetworkPolicy_NoProposal verifies that an empty store also defaults
// to no-network.
func TestSkillNetworkPolicy_NoProposal(t *testing.T) {
	store := testProposalStore(t)
	env := &runtimeEnv{ProposalStore: store}
	pol := skillNetworkPolicy("my-skill", env)
	if !pol.NoNetwork {
		t.Errorf("expected NoNetwork=true when no proposals, got %+v", pol)
	}
}

// TestSkillNetworkPolicy_ApprovedNoNetworkCap verifies that an approved proposal
// with Capabilities.Network=false still produces NoNetwork policy.
func TestSkillNetworkPolicy_ApprovedNoNetworkCap(t *testing.T) {
	store := testProposalStore(t)
	makeApprovedProposal(t, store, "safe-skill",
		&proposal.SkillCapabilities{Network: false},
		nil,
	)
	env := &runtimeEnv{ProposalStore: store}
	pol := skillNetworkPolicy("safe-skill", env)
	if !pol.NoNetwork {
		t.Errorf("expected NoNetwork=true for Capabilities.Network=false, got %+v", pol)
	}
}

// TestSkillNetworkPolicy_ApprovedWithNetworkCap verifies that an approved
// proposal with Capabilities.Network=true and an explicit NetworkPolicy is
// translated into a permissive SandboxNetworkPolicy.
func TestSkillNetworkPolicy_ApprovedWithNetworkCap(t *testing.T) {
	store := testProposalStore(t)
	makeApprovedProposal(t, store, "net-skill",
		&proposal.SkillCapabilities{Network: true},
		&proposal.ProposalNetworkPolicy{
			DefaultDeny:  true,
			AllowedHosts: []string{"api.example.com"},
			AllowedPorts: []uint16{443},
		},
	)
	env := &runtimeEnv{ProposalStore: store}
	pol := skillNetworkPolicy("net-skill", env)

	if pol.NoNetwork {
		t.Error("expected NoNetwork=false when Capabilities.Network=true")
	}
	if !pol.DefaultDeny {
		t.Error("expected DefaultDeny=true (hard invariant)")
	}
	if len(pol.AllowedHosts) != 1 || pol.AllowedHosts[0] != "api.example.com" {
		t.Errorf("unexpected AllowedHosts: %v", pol.AllowedHosts)
	}
	if len(pol.AllowedPorts) != 1 || pol.AllowedPorts[0] != 443 {
		t.Errorf("unexpected AllowedPorts: %v", pol.AllowedPorts)
	}
}

// TestSkillNetworkPolicy_WrongSkillName verifies that a proposal for a
// different skill does not affect the policy for the requested skill.
func TestSkillNetworkPolicy_WrongSkillName(t *testing.T) {
	store := testProposalStore(t)
	makeApprovedProposal(t, store, "other-skill",
		&proposal.SkillCapabilities{Network: true},
		nil,
	)
	env := &runtimeEnv{ProposalStore: store}
	pol := skillNetworkPolicy("my-skill", env)
	if !pol.NoNetwork {
		t.Errorf("expected NoNetwork=true when no matching proposal, got %+v", pol)
	}
}

// TestSkillNetworkPolicy_DefaultsSandboxNetworkPolicy makes sure the default
// sandbox.NetworkPolicy zero-value is safe (all-false / empty).
func TestSkillNetworkPolicy_DefaultsSandboxNetworkPolicy(t *testing.T) {
	var pol sandbox.NetworkPolicy
	if pol.NoNetwork || pol.DefaultDeny || len(pol.AllowedHosts) != 0 {
		t.Errorf("expected zero-value NetworkPolicy to have all fields false/empty: %+v", pol)
	}
}

// TestSkillNetworkPolicy_NeverReturnsUnsafeZeroValue verifies that every code
// path in skillNetworkPolicy returns either NoNetwork=true or DefaultDeny=true.
// This prevents accidentally granting unrestricted network access.
func TestSkillNetworkPolicy_NeverReturnsUnsafeZeroValue(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T) *runtimeEnv
	}{
		{
			name: "nil store",
			setup: func(t *testing.T) *runtimeEnv {
				return &runtimeEnv{ProposalStore: nil}
			},
		},
		{
			name: "empty store",
			setup: func(t *testing.T) *runtimeEnv {
				return &runtimeEnv{ProposalStore: testProposalStore(t)}
			},
		},
		{
			name: "no-network capability",
			setup: func(t *testing.T) *runtimeEnv {
				store := testProposalStore(t)
				makeApprovedProposal(t, store, "s", &proposal.SkillCapabilities{Network: false}, nil)
				return &runtimeEnv{ProposalStore: store}
			},
		},
		{
			name: "network capability with policy",
			setup: func(t *testing.T) *runtimeEnv {
				store := testProposalStore(t)
				makeApprovedProposal(t, store, "s",
					&proposal.SkillCapabilities{Network: true},
					&proposal.ProposalNetworkPolicy{DefaultDeny: true, AllowedHosts: []string{"a.example.com"}},
				)
				return &runtimeEnv{ProposalStore: store}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := tc.setup(t)
			pol := skillNetworkPolicy("s", env)
			if !pol.NoNetwork && !pol.DefaultDeny {
				t.Errorf("unsafe NetworkPolicy returned (NoNetwork=false, DefaultDeny=false): %+v", pol)
			}
		})
	}
}
