package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

// vaultTimeFormat is the display format used for secret metadata timestamps.
const vaultTimeFormat = "2006-01-02 15:04"

// makeVaultSecretAddHandler returns a handler for the "vault.secret.add" action.
// The daemon owns all vault writes; the CLI sends the plaintext over the local
// Unix socket and the daemon encrypts it before writing to disk.
// When req.Rotate is true the secret must already exist (rotate semantics).
func makeVaultSecretAddHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.VaultSecretAddRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Name == "" {
			return &api.Response{Error: "name is required"}
		}
		if req.SkillID == "" {
			return &api.Response{Error: "skill_id is required"}
		}
		if req.Value == "" {
			return &api.Response{Error: "value is required"}
		}
		if env.Vault == nil {
			return &api.Response{Error: "vault is not available"}
		}

		if req.Rotate {
			// Rotate requires the secret to exist already.
			if !env.Vault.Has(req.Name) {
				return &api.Response{Error: fmt.Sprintf("secret %q not found — use 'secrets add' to create it", req.Name)}
			}
		}

		if err := env.Vault.Add(req.Name, req.SkillID, []byte(req.Value)); err != nil {
			return &api.Response{Error: "failed to store secret: " + err.Error()}
		}

		action := kernel.NewAction(kernel.ActionSecretAdd, "cli",
			fmt.Appendf(nil, `{"name":%q,"skill_id":%q,"rotate":%v}`, req.Name, req.SkillID, req.Rotate))
		if _, logErr := env.Kernel.SignAndLog(action); logErr != nil {
			env.Logger.Error("failed to audit log secret add", zap.String("name", req.Name), zap.Error(logErr))
		}

		verb := "stored"
		if req.Rotate {
			verb = "rotated"
		}
		return &api.Response{
			Success: true,
			Data:    mustMarshal(map[string]string{"message": fmt.Sprintf("secret %q %s for skill %q", req.Name, verb, req.SkillID)}),
		}
	}
}

// makeVaultSecretListHandler returns a handler for the "vault.secret.list" action.
// It returns metadata only — the plaintext is never included.
func makeVaultSecretListHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.VaultSecretListRequest
		// data may be nil/empty for an unfiltered list; ignore parse errors.
		if len(data) > 0 {
			_ = json.Unmarshal(data, &req)
		}
		if env.Vault == nil {
			return &api.Response{Error: "vault is not available"}
		}

		var rawEntries interface{}
		if req.SkillID != "" {
			rawEntries = env.Vault.ListForSkill(req.SkillID)
		} else {
			rawEntries = env.Vault.List()
		}

		// Convert to the API type (avoids leaking vault internals).
		entries := toVaultSecretEntries(rawEntries)
		return &api.Response{
			Success: true,
			Data:    mustMarshal(entries),
		}
	}
}

// makeVaultSecretDeleteHandler returns a handler for the "vault.secret.delete" action.
func makeVaultSecretDeleteHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.VaultSecretDeleteRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Name == "" {
			return &api.Response{Error: "name is required"}
		}
		if env.Vault == nil {
			return &api.Response{Error: "vault is not available"}
		}
		if err := env.Vault.Delete(req.Name); err != nil {
			return &api.Response{Error: "failed to delete secret: " + err.Error()}
		}

		action := kernel.NewAction(kernel.ActionSecretDelete, "cli",
			fmt.Appendf(nil, `{"name":%q}`, req.Name))
		if _, logErr := env.Kernel.SignAndLog(action); logErr != nil {
			env.Logger.Error("failed to audit log secret delete", zap.String("name", req.Name), zap.Error(logErr))
		}

		return &api.Response{
			Success: true,
			Data:    mustMarshal(map[string]string{"message": fmt.Sprintf("secret %q deleted", req.Name)}),
		}
	}
}

// makeVaultSecretRotateHandler returns a handler for the "vault.secret.rotate" action.
// Unlike vault.secret.add, rotate requires the secret to already exist and
// preserves the existing skill_id when the caller does not supply one.
func makeVaultSecretRotateHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req api.VaultSecretAddRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}
		if req.Name == "" {
			return &api.Response{Error: "name is required"}
		}
		if req.Value == "" {
			return &api.Response{Error: "value is required"}
		}
		if env.Vault == nil {
			return &api.Response{Error: "vault is not available"}
		}

		// Rotate requires the secret to already exist.
		existing, ok := env.Vault.GetEntry(req.Name)
		if !ok {
			return &api.Response{Error: fmt.Sprintf("secret %q not found — use 'secrets add' to create it", req.Name)}
		}

		// Preserve existing skill association when none was supplied.
		skillID := req.SkillID
		if skillID == "" {
			skillID = existing.SkillID
		}

		if err := env.Vault.Add(req.Name, skillID, []byte(req.Value)); err != nil {
			return &api.Response{Error: "failed to rotate secret: " + err.Error()}
		}

		action := kernel.NewAction(kernel.ActionSecretAdd, "cli",
			fmt.Appendf(nil, `{"name":%q,"skill_id":%q,"action":"rotate"}`, req.Name, skillID))
		if _, logErr := env.Kernel.SignAndLog(action); logErr != nil {
			env.Logger.Error("failed to audit log secret rotation", zap.String("name", req.Name), zap.Error(logErr))
		}

		return &api.Response{
			Success: true,
			Data:    mustMarshal(map[string]string{"message": fmt.Sprintf("secret %q rotated for skill %q", req.Name, skillID)}),
		}
	}
}

// Using a concrete type avoids an import cycle; we duck-type via JSON round-trip.
func toVaultSecretEntries(rawEntries interface{}) []api.VaultSecretEntry {
	b, err := json.Marshal(rawEntries)
	if err != nil {
		return nil
	}
	// Parse into a generic slice to extract fields present in both types.
	var raw []struct {
		Name      string    `json:"name"`
		SkillID   string    `json:"skill_id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Size      int       `json:"size"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil
	}
	out := make([]api.VaultSecretEntry, len(raw))
	for i, e := range raw {
		out[i] = api.VaultSecretEntry{
			Name:      e.Name,
			SkillID:   e.SkillID,
			CreatedAt: e.CreatedAt.Format(vaultTimeFormat),
			UpdatedAt: e.UpdatedAt.Format(vaultTimeFormat),
			Size:      e.Size,
		}
	}
	return out
}

// mustMarshal marshals v to JSON, returning nil on error (handler bugs only).
func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
