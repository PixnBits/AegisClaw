package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/vault"
	"go.uber.org/zap"
)

// makeTestVault creates a temporary vault with a fresh Ed25519 key.
func makeTestVault(t *testing.T) (*vault.Vault, ed25519.PrivateKey) {
	t.Helper()
	dir := t.TempDir()
	logger := zap.NewNop()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	v, err := vault.NewVault(dir, priv, logger)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}
	return v, priv
}

// makeTestEnvWithVault builds a minimal runtimeEnv with a vault and proposal
// store loaded for pre-activation checks.
func makeTestEnvWithVault(t *testing.T) *runtimeEnv {
	t.Helper()

	v, _ := makeTestVault(t)
	store := testProposalStore(t)
	cfg := &config.Config{}
	cfg.Vault.Dir = t.TempDir()

	return &runtimeEnv{
		Logger:        zap.NewNop(),
		Config:        cfg,
		Vault:         v,
		ProposalStore: store,
	}
}

// TestCheckSecretsBeforeActivate_NoProposal verifies that when there is no
// approved proposal for a skill, the check passes without error (no secrets
// required means no validation needed).
func TestCheckSecretsBeforeActivate_NoProposal(t *testing.T) {
	v, _ := makeTestVault(t)
	store := testProposalStore(t)

	cfg := &config.Config{}
	cfg.Vault.Dir = t.TempDir()

	env := &runtimeEnv{
		Logger:        zap.NewNop(),
		Config:        cfg,
		Vault:         v,
		ProposalStore: store,
	}

	// No proposals in store — should return nil (no secrets required).
	if err := checkSecretsBeforeActivate("my-skill", env); err != nil {
		t.Errorf("expected nil error for no-proposal case, got: %v", err)
	}
}

// TestCheckSecretsBeforeActivate_AllPresent verifies that when all declared
// secrets exist in the vault, the pre-activation check passes.
func TestCheckSecretsBeforeActivate_AllPresent(t *testing.T) {
	v, _ := makeTestVault(t)
	store := testProposalStore(t)

	// Add the secrets to the vault.
	if err := v.Add("DISCORD_BOT_TOKEN", "discord-skill", []byte("tok-123")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := v.Add("DISCORD_GUILD_ID", "discord-skill", []byte("gid-456")); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Create an approved proposal that declares those secrets.
	makeApprovedProposal(t, store, "discord-skill",
		&proposal.SkillCapabilities{Network: true, Secrets: []string{"DISCORD_BOT_TOKEN", "DISCORD_GUILD_ID"}},
		&proposal.ProposalNetworkPolicy{DefaultDeny: true, AllowedHosts: []string{"discord.com"}},
	)
	// Also set SecretsRefs on the proposal via direct import.
	summaries, _ := store.List()
	if len(summaries) > 0 {
		p, err := store.Get(summaries[0].ID)
		if err == nil {
			p.SecretsRefs = []string{"DISCORD_BOT_TOKEN", "DISCORD_GUILD_ID"}
			_ = store.Update(p)
		}
	}

	cfg := &config.Config{}
	cfg.Vault.Dir = t.TempDir()

	env := &runtimeEnv{
		Logger:        zap.NewNop(),
		Config:        cfg,
		Vault:         v,
		ProposalStore: store,
	}

	if err := checkSecretsBeforeActivate("discord-skill", env); err != nil {
		t.Errorf("expected nil error when all secrets present, got: %v", err)
	}
}

// TestCheckSecretsBeforeActivate_Missing verifies that when a secret is
// missing from the vault, the pre-activation check returns an informative error.
func TestCheckSecretsBeforeActivate_Missing(t *testing.T) {
	v, _ := makeTestVault(t)
	store := testProposalStore(t)

	// Intentionally do NOT add DISCORD_BOT_TOKEN to the vault.

	p, err := proposal.NewProposal("discord skill", "sends Discord messages", proposal.CategoryNewSkill, "tester")
	if err != nil {
		t.Fatalf("NewProposal: %v", err)
	}
	p.TargetSkill = "discord-skill"
	p.Status = proposal.StatusApproved
	p.SecretsRefs = []string{"DISCORD_BOT_TOKEN"}
	p.Capabilities = &proposal.SkillCapabilities{Secrets: []string{"DISCORD_BOT_TOKEN"}}
	if err := store.Import(p); err != nil {
		t.Fatalf("Import: %v", err)
	}

	cfg := &config.Config{}
	cfg.Vault.Dir = t.TempDir()

	env := &runtimeEnv{
		Logger:        zap.NewNop(),
		Config:        cfg,
		Vault:         v,
		ProposalStore: store,
	}

	err = checkSecretsBeforeActivate("discord-skill", env)
	if err == nil {
		t.Fatal("expected error when DISCORD_BOT_TOKEN is missing from vault")
	}
	errMsg := err.Error()
	if !contains(errMsg, "DISCORD_BOT_TOKEN") {
		t.Errorf("error message should mention the missing secret, got: %s", errMsg)
	}
	if !contains(errMsg, "aegisclaw secrets add") {
		t.Errorf("error message should include the add command hint, got: %s", errMsg)
	}
}

// TestCheckSecretsBeforeActivate_NilVault verifies that a nil vault causes
// the check to pass (daemon handles it at injection time).
func TestCheckSecretsBeforeActivate_NilVault(t *testing.T) {
	store := testProposalStore(t)

	p, err := proposal.NewProposal("test skill", "test", proposal.CategoryNewSkill, "tester")
	if err != nil {
		t.Fatalf("NewProposal: %v", err)
	}
	p.TargetSkill = "my-skill"
	p.Status = proposal.StatusApproved
	p.SecretsRefs = []string{"MY_SECRET"}
	_ = store.Import(p)

	env := &runtimeEnv{
		Logger:        zap.NewNop(),
		Config:        &config.Config{},
		Vault:         nil, // nil vault
		ProposalStore: store,
	}

	// nil vault: should return nil without panicking.
	if err := checkSecretsBeforeActivate("my-skill", env); err != nil {
		t.Errorf("expected nil when vault is nil, got: %v", err)
	}
}

// TestInjectSecretsIntoVM_NoRefs verifies that calling injectSecretsIntoVM with
// an empty refs list is a no-op returning 0 injected and no error.
func TestInjectSecretsIntoVM_NoRefs(t *testing.T) {
	env := &runtimeEnv{
		Logger: zap.NewNop(),
	}
	// Use doInjectSecrets directly; sender is never called for empty refs.
	n, err := doInjectSecrets(nil, env, "sandbox-1", "my-skill", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 injected, got %d", n)
	}
}

// TestInjectSecretsIntoVM_NilVault verifies that a nil vault returns an error.
func TestInjectSecretsIntoVM_NilVault(t *testing.T) {
	env := &runtimeEnv{
		Logger: zap.NewNop(),
		Vault:  nil,
	}
	// sender is never called when the vault is nil.
	_, err := doInjectSecrets(nil, env, "sandbox-1", "my-skill", []string{"MY_SECRET"}, nil)
	if err == nil {
		t.Fatal("expected error when vault is nil")
	}
}

// TestInjectSecretsIntoVM_MissingSecret verifies that a missing secret returns
// an error mentioning the missing secret name and the CLI add command.
func TestInjectSecretsIntoVM_MissingSecret(t *testing.T) {
	v, _ := makeTestVault(t)
	// Intentionally don't add MY_SECRET.

	env := &runtimeEnv{
		Logger: zap.NewNop(),
		Vault:  v,
	}

	// sender is never called when all secrets are missing from vault.
	_, err := doInjectSecrets(nil, env, "sandbox-1", "my-skill", []string{"MY_SECRET"}, nil)
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
	if !contains(err.Error(), "aegisclaw secrets add") {
		t.Errorf("error should hint at add command, got: %v", err)
	}
}

// TestInjectSecretsIntoVM_SecretPresentNoRuntime verifies that the public
// wrapper returns an error when the Runtime field is nil.
func TestInjectSecretsIntoVM_SecretPresentNoRuntime(t *testing.T) {
	v, _ := makeTestVault(t)
	if err := v.Add("API_KEY", "my-skill", []byte("secret-value")); err != nil {
		t.Fatalf("Add: %v", err)
	}

	env := &runtimeEnv{
		Logger:  zap.NewNop(),
		Vault:   v,
		Runtime: nil, // no runtime — public wrapper must catch this
	}

	_, err := injectSecretsIntoVM(nil, env, "sandbox-1", "my-skill", []string{"API_KEY"})
	if err == nil {
		t.Fatal("expected error when runtime is nil")
	}
	if !contains(err.Error(), "runtime not available") {
		t.Errorf("expected 'runtime not available' message, got: %v", err)
	}
}

// TestDoInjectSecrets_RoundTrip is the full secrets-injection round-trip test
// using a mock vmSendFunc.  It verifies that:
//   - secrets present in the vault are resolved and sent to the VM
//   - the vsock payload contains a "secrets.inject" type message
//   - the correct sandbox ID is passed to the sender
//   - the returned count matches the number of secrets injected
func TestDoInjectSecrets_RoundTrip(t *testing.T) {
	v, _ := makeTestVault(t)
	if err := v.Add("BOT_TOKEN", "discord-skill", []byte("tok-abc")); err != nil {
		t.Fatalf("Add BOT_TOKEN: %v", err)
	}
	if err := v.Add("GUILD_ID", "discord-skill", []byte("gid-xyz")); err != nil {
		t.Fatalf("Add GUILD_ID: %v", err)
	}

	env := &runtimeEnv{
		Logger: zap.NewNop(),
		Vault:  v,
	}

	var capturedID string
	var capturedMsg map[string]interface{}

	mockSender := vmSendFunc(func(_ context.Context, sandboxID string, req interface{}) (json.RawMessage, error) {
		capturedID = sandboxID
		// Marshal + unmarshal so we can inspect the message fields.
		raw, _ := json.Marshal(req)
		_ = json.Unmarshal(raw, &capturedMsg)
		// Return a success response matching the guest-agent's ack format.
		ack, _ := json.Marshal(map[string]interface{}{"success": true})
		return json.RawMessage(ack), nil
	})

	n, err := doInjectSecrets(context.Background(), env, "vm-sandbox-99", "discord-skill",
		[]string{"BOT_TOKEN", "GUILD_ID"}, mockSender)
	if err != nil {
		t.Fatalf("doInjectSecrets returned error: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 secrets injected, got %d", n)
	}
	if capturedID != "vm-sandbox-99" {
		t.Errorf("expected sandbox ID vm-sandbox-99, got %q", capturedID)
	}
	if capturedMsg["type"] != "secrets.inject" {
		t.Errorf("expected message type 'secrets.inject', got %q", capturedMsg["type"])
	}
}

// TestDoInjectSecrets_SendFailure verifies that a sender error is propagated.
func TestDoInjectSecrets_SendFailure(t *testing.T) {
	v, _ := makeTestVault(t)
	if err := v.Add("MY_TOKEN", "my-skill", []byte("tok-123")); err != nil {
		t.Fatalf("Add: %v", err)
	}

	env := &runtimeEnv{
		Logger: zap.NewNop(),
		Vault:  v,
	}

	failSender := vmSendFunc(func(_ context.Context, _ string, _ interface{}) (json.RawMessage, error) {
		return nil, fmt.Errorf("vsock connection refused")
	})

	_, err := doInjectSecrets(context.Background(), env, "sb-1", "my-skill",
		[]string{"MY_TOKEN"}, failSender)
	if err == nil {
		t.Fatal("expected error when sender fails")
	}
	if !contains(err.Error(), "vsock inject") {
		t.Errorf("expected vsock inject error, got: %v", err)
	}
}

// contains is a helper for substring checks in error messages.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// TestSecretsRefreshHandler_SkillNotActive verifies the refresh handler returns an error
// when the skill is not currently active in the registry.
func TestSecretsRefreshHandler_SkillNotActive(t *testing.T) {
	v, _ := makeTestVault(t)
	store := testProposalStore(t)
	cfg := &config.Config{}
	cfg.Vault.Dir = t.TempDir()
	reg, err := sandbox.NewSkillRegistry(t.TempDir() + "/registry.json")
	if err != nil {
		t.Fatalf("NewSkillRegistry: %v", err)
	}

	env := &runtimeEnv{
		Logger:        zap.NewNop(),
		Config:        cfg,
		Vault:         v,
		ProposalStore: store,
		Registry:      reg,
	}

	handler := makeSecretsRefreshHandler(env)
	reqData, _ := json.Marshal(map[string]string{"name": "nonexistent-skill"})
	resp := handler(context.Background(), reqData)
	if resp.Error == "" {
		t.Fatal("expected error for non-active skill, got success")
	}
	if !strings.Contains(resp.Error, "not currently active") {
		t.Errorf("unexpected error message: %q", resp.Error)
	}
}
