package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap/zaptest"
)

// testEnvWithVaultAndKernel creates a runtimeEnv with a real vault and kernel.
func testEnvWithVaultAndKernel(t *testing.T) *runtimeEnv {
	t.Helper()
	kernel.ResetInstance()
	logger := zaptest.NewLogger(t)

	kern, err := kernel.GetInstance(logger, t.TempDir())
	if err != nil {
		t.Fatalf("kernel.GetInstance: %v", err)
	}
	t.Cleanup(func() {
		kern.Shutdown()
		kernel.ResetInstance()
	})

	v, _ := makeTestVault(t)
	return &runtimeEnv{
		Logger: logger,
		Kernel: kern,
		Vault:  v,
	}
}

// callVaultHandler is a small helper that marshals req, calls the handler, and
// returns the response.
func callVaultHandler(t *testing.T, h func(context.Context, json.RawMessage) *api.Response, req any) *api.Response {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return h(context.Background(), data)
}

// ---------- vault.secret.add ----------

func TestVaultSecretAddHandler_HappyPath(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)
	h := makeVaultSecretAddHandler(env)

	resp := callVaultHandler(t, h, api.VaultSecretAddRequest{
		Name:    "mytoken",
		SkillID: "skill-a",
		Value:   "supersecret",
	})

	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}
	if !env.Vault.Has("mytoken") {
		t.Fatal("expected secret to be stored in vault")
	}
}

func TestVaultSecretAddHandler_MissingFields(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)
	h := makeVaultSecretAddHandler(env)

	cases := []api.VaultSecretAddRequest{
		{Name: "", SkillID: "s", Value: "v"},
		{Name: "n", SkillID: "", Value: "v"},
		{Name: "n", SkillID: "s", Value: ""},
	}
	for _, req := range cases {
		resp := callVaultHandler(t, h, req)
		if resp.Success {
			t.Errorf("expected error for %+v, got success", req)
		}
	}
}

func TestVaultSecretAddHandler_NilVault(t *testing.T) {
	env := &runtimeEnv{Vault: nil}
	h := makeVaultSecretAddHandler(env)
	resp := callVaultHandler(t, h, api.VaultSecretAddRequest{Name: "n", SkillID: "s", Value: "v"})
	if resp.Success {
		t.Fatal("expected error when vault is nil")
	}
}

func TestVaultSecretAddHandler_RotateRejectsNewSecret(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)
	h := makeVaultSecretAddHandler(env)

	resp := callVaultHandler(t, h, api.VaultSecretAddRequest{
		Name:    "nonexistent",
		SkillID: "s",
		Value:   "v",
		Rotate:  true,
	})
	if resp.Success {
		t.Fatal("expected error rotating a nonexistent secret")
	}
}

func TestVaultSecretAddHandler_RotateExistingSecret(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)

	// Pre-populate.
	if err := env.Vault.Add("tok", "skill-x", []byte("oldval")); err != nil {
		t.Fatalf("pre-populate: %v", err)
	}

	h := makeVaultSecretAddHandler(env)
	resp := callVaultHandler(t, h, api.VaultSecretAddRequest{
		Name:    "tok",
		SkillID: "skill-x",
		Value:   "newval",
		Rotate:  true,
	})
	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Error)
	}
}

// ---------- vault.secret.rotate ----------

func TestVaultSecretRotateHandler_PreservesSkillID(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)

	if err := env.Vault.Add("apikey", "original-skill", []byte("v1")); err != nil {
		t.Fatalf("pre-populate: %v", err)
	}

	h := makeVaultSecretRotateHandler(env)
	// Call rotate without supplying a skill_id — daemon should preserve existing.
	resp := callVaultHandler(t, h, api.VaultSecretAddRequest{
		Name:  "apikey",
		Value: "v2",
		// SkillID intentionally empty
	})
	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Error)
	}

	entry, ok := env.Vault.GetEntry("apikey")
	if !ok {
		t.Fatal("entry should still exist after rotate")
	}
	if entry.SkillID != "original-skill" {
		t.Fatalf("expected skill_id %q, got %q", "original-skill", entry.SkillID)
	}
}

func TestVaultSecretRotateHandler_RejectsNewSecret(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)
	h := makeVaultSecretRotateHandler(env)

	resp := callVaultHandler(t, h, api.VaultSecretAddRequest{
		Name:  "ghost",
		Value: "v",
	})
	if resp.Success {
		t.Fatal("expected error rotating nonexistent secret")
	}
}

// ---------- vault.secret.list ----------

func TestVaultSecretListHandler_Empty(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)
	h := makeVaultSecretListHandler(env)

	resp := callVaultHandler(t, h, api.VaultSecretListRequest{})
	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Error)
	}
	var entries []api.VaultSecretEntry
	if err := json.Unmarshal(resp.Data, &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestVaultSecretListHandler_ReturnsMetadataOnly(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)

	env.Vault.Add("sec1", "skill-a", []byte("value1"))
	env.Vault.Add("sec2", "skill-b", []byte("value2"))

	h := makeVaultSecretListHandler(env)
	resp := callVaultHandler(t, h, api.VaultSecretListRequest{})
	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Error)
	}

	var entries []api.VaultSecretEntry
	if err := json.Unmarshal(resp.Data, &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Values must never appear in the response.
	raw := string(resp.Data)
	for _, v := range []string{"value1", "value2"} {
		if containsStr(raw, v) {
			t.Errorf("response contains plaintext value %q", v)
		}
	}
}

func TestVaultSecretListHandler_FilterBySkill(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)

	env.Vault.Add("a", "skill-x", []byte("v1"))
	env.Vault.Add("b", "skill-y", []byte("v2"))
	env.Vault.Add("c", "skill-x", []byte("v3"))

	h := makeVaultSecretListHandler(env)
	resp := callVaultHandler(t, h, api.VaultSecretListRequest{SkillID: "skill-x"})
	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Error)
	}

	var entries []api.VaultSecretEntry
	json.Unmarshal(resp.Data, &entries)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for skill-x, got %d", len(entries))
	}
}

func TestVaultSecretListHandler_NilVault(t *testing.T) {
	env := &runtimeEnv{Vault: nil}
	h := makeVaultSecretListHandler(env)
	resp := callVaultHandler(t, h, api.VaultSecretListRequest{})
	if resp.Success {
		t.Fatal("expected error when vault is nil")
	}
}

// ---------- vault.secret.delete ----------

func TestVaultSecretDeleteHandler_HappyPath(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)

	env.Vault.Add("todel", "s", []byte("v"))
	h := makeVaultSecretDeleteHandler(env)

	resp := callVaultHandler(t, h, api.VaultSecretDeleteRequest{Name: "todel"})
	if !resp.Success {
		t.Fatalf("expected success, got: %s", resp.Error)
	}
	if env.Vault.Has("todel") {
		t.Fatal("secret should be removed after delete")
	}
}

func TestVaultSecretDeleteHandler_Nonexistent(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)
	h := makeVaultSecretDeleteHandler(env)

	resp := callVaultHandler(t, h, api.VaultSecretDeleteRequest{Name: "ghost"})
	if resp.Success {
		t.Fatal("expected error deleting nonexistent secret")
	}
}

func TestVaultSecretDeleteHandler_MissingName(t *testing.T) {
	env := testEnvWithVaultAndKernel(t)
	h := makeVaultSecretDeleteHandler(env)

	resp := callVaultHandler(t, h, api.VaultSecretDeleteRequest{Name: ""})
	if resp.Success {
		t.Fatal("expected error for empty name")
	}
}

func TestVaultSecretDeleteHandler_NilVault(t *testing.T) {
	env := &runtimeEnv{Vault: nil}
	h := makeVaultSecretDeleteHandler(env)
	resp := callVaultHandler(t, h, api.VaultSecretDeleteRequest{Name: "x"})
	if resp.Success {
		t.Fatal("expected error when vault is nil")
	}
}

// containsStr is a simple substring check for test assertions.
func containsStr(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) &&
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}()
}
