package main

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"AegisClaw/internal/storegit"
)

func payloadMap(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok && m != nil {
		return m
	}
	return map[string]interface{}{}
}

func payloadString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func payloadBool(m map[string]interface{}, key string) bool {
	switch v := m[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	default:
		return false
	}
}

func isCoderSource(source string) bool {
	s := strings.ToLower(strings.TrimSpace(source))
	return s == "coder" || strings.HasPrefix(s, "coder-")
}

func coderNoGit() (string, interface{}) {
	return "error", "coder actor has no git"
}

func tenancyDeny(detail string) (string, interface{}) {
	return "error", "tenancy acl: not your tenant (" + detail + ")"
}

func remotePayload(tenant, repo string) map[string]interface{} {
	return map[string]interface{}{
		"remote": storegit.RemoteURL(tenant, repo),
		"tenant": tenant,
		"repo":   repo,
	}
}

func handleGitCloneRPC(source string, raw interface{}) (string, interface{}) {
	if isCoderSource(source) {
		return coderNoGit()
	}
	p := payloadMap(raw)
	tenant := payloadString(p, "tenant")
	repo := payloadString(p, "repo")
	if tenant == "" || repo == "" {
		return "error", "tenant and repo required"
	}
	from := payloadString(p, "from_tenant")
	if from != "" && from != tenant {
		return tenancyDeny("from_tenant " + from + " is not " + tenant)
	}
	for _, k := range []string{"remote", "target"} {
		if s := payloadString(p, k); s != "" {
			if t, _, ok := storegit.ParseURL(s); ok && t != tenant {
				return tenancyDeny("remote belongs to " + t)
			}
		}
	}
	if _, err := ensureBareRepo(tenant, repo); err != nil {
		return "error", err.Error()
	}
	return "git.cloned", remotePayload(tenant, repo)
}

func handleGitCreateRPC(source string, raw interface{}) (string, interface{}) {
	if isCoderSource(source) {
		return coderNoGit()
	}
	p := payloadMap(raw)
	tenant := payloadString(p, "tenant")
	repo := payloadString(p, "repo", "id", "name")
	if tenant == "" || repo == "" {
		return "error", "tenant and repo required"
	}
	if _, err := ensureBareRepo(tenant, repo); err != nil {
		return "error", err.Error()
	}
	return "git.created", remotePayload(tenant, repo)
}

func handleSkillCreateRPC(source string, raw interface{}, skills map[string]interface{}) (string, interface{}) {
	if isCoderSource(source) {
		return coderNoGit()
	}
	p := payloadMap(raw)
	tenant := payloadString(p, "tenant")
	repo := payloadString(p, "repo", "id", "name")
	id := payloadString(p, "id", "repo", "name")
	if tenant == "" || repo == "" {
		return "error", "tenant and repo required"
	}
	if _, err := ensureBareRepo(tenant, repo); err != nil {
		return "error", err.Error()
	}
	if id != "" {
		rec := map[string]interface{}{}
		for k, v := range p {
			rec[k] = v
		}
		rec["id"] = id
		rec["repo"] = repo
		rec["tenant"] = tenant
		rec["remote"] = storegit.RemoteURL(tenant, repo)
		skills[id] = rec
		saveToFile("skills.json", skills)
	}
	return "skill.created", remotePayload(tenant, repo)
}

func handleGitPushRPC(source string, raw interface{}) (string, interface{}) {
	if isCoderSource(source) {
		return coderNoGit()
	}
	p := payloadMap(raw)
	tenant := payloadString(p, "tenant")
	targetTenant := payloadString(p, "target_tenant", "to_tenant")
	if targetTenant != "" && tenant != "" && targetTenant != tenant {
		return tenancyDeny("cannot push to target_tenant " + targetTenant)
	}
	if extra := payloadString(p, "extra_remote"); extra != "" || payloadBool(p, "submodule") ||
		payloadBool(p, "gitmodules") || payloadBool(p, "lfs") || payloadBool(p, "hooks") {
		return "error", "extra remote, submodule, lfs, and hook denied"
	}
	refspec := payloadString(p, "refspec")
	if payloadBool(p, "force") || strings.HasPrefix(refspec, "+") {
		return "error", "force-push and history rewrite denied (fast-forward only)"
	}
	if payloadBool(p, "delete_refs") || strings.HasPrefix(refspec, ":") {
		return "error", "delete-refs / history rewrite denied"
	}
	if i := strings.Index(refspec, ":"); i == 0 || (i > 0 && strings.TrimSpace(refspec[:i]) == "") {
		return "error", "delete-refs / history rewrite denied"
	}
	// Real objects arrive via git(1) push to the hub::vsock remote, not JSON pack.
	return "error", "git push is via the hub vsock remote, not a JSON pack"
}

func handlePRMergeRPC(source string, raw interface{}, prs, proposals map[string]interface{}) (string, interface{}) {
	src := strings.ToLower(strings.TrimSpace(source))
	if src != "store" {
		return "error", "wrong actor: only store may merge (store only)"
	}
	p := payloadMap(raw)
	id := payloadString(p, "id", "pr_id")
	var pr map[string]interface{}
	if id != "" {
		pr, _ = prs[id].(map[string]interface{})
	}
	propID := payloadString(p, "proposal_id")
	if propID == "" && pr != nil {
		propID = payloadString(pr, "proposal_id")
	}
	if propID == "" {
		return "error", "court not approved"
	}
	prop, _ := proposals[propID].(map[string]interface{})
	if prop == nil {
		return "error", "court not approved"
	}
	state, _ := prop["state"].(string)
	if !strings.EqualFold(state, "approved") {
		return "error", "court not approved"
	}
	cd, _ := prop["court_decision"].(map[string]interface{})
	if cd == nil {
		return "error", "court decision unsigned: merkle signature required"
	}
	_, hasMerkle := cd["decision_merkle"]
	sig := payloadString(cd, "decision_sig", "sig", "signature")
	if !hasMerkle || sig == "" {
		return "error", "court decision unsigned: merkle signature required"
	}
	if pr != nil {
		pr["merged"] = true
		prs[id] = pr
		saveToFile("prs.json", prs)
	}
	return "pr.merged", map[string]interface{}{"id": id, "proposal_id": propID}
}

func handlePRRollbackRPC(raw interface{}, prs map[string]interface{}) (string, interface{}) {
	p := payloadMap(raw)
	oldID := payloadString(p, "id", "pr_id")
	newID := oldID + "-rollback-" + fmt.Sprintf("%d", time.Now().UnixNano())
	if oldID == "" {
		newID = "pr-rollback-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	rec := map[string]interface{}{
		"id":     newID,
		"repo":   payloadString(p, "repo"),
		"tenant": payloadString(p, "tenant"),
		"title":  "rollback",
		"court":  "pending",
		"state":  "pending",
		"ref":    payloadString(p, "ref"),
	}
	if oldID != "" {
		rec["rollback_of"] = oldID
	}
	prs[newID] = rec
	saveToFile("prs.json", prs)
	return "pr.rollback", rec
}

func wipeBuilderLeftovers() {
	for _, name := range []string{".git-credentials", ".netrc", "id_rsa", "id_ed25519"} {
		_ = os.Remove(name)
	}
	_ = os.RemoveAll("builder-work")
}

func builderStopID(raw interface{}) string {
	p := payloadMap(raw)
	if id := payloadString(p, "id", "builder_id", "vm_id"); id != "" {
		return id
	}
	return "builder"
}

// requestOrchestratorStopVM asks the host daemon to StopVM the Builder.
// Store-cwd wipe (wipeBuilderLeftovers / T11) stays in addition, not instead.
// The daemon unlinks the private Firecracker rootfs only after backend.Stop
// (guest unmounted); this must not wipe/unlink the img before Stop (EBUSY).
func requestOrchestratorStopVM(encoder *json.Encoder, priv ed25519.PrivateKey, ts string, raw interface{}) {
	if encoder == nil {
		return
	}
	id := builderStopID(raw)
	msg := Message{
		Source:      "store",
		Destination: "daemon-orchestrator",
		Command:     "orchestrator.stop_vm",
		Payload:     map[string]interface{}{"id": id, "builder_id": id},
		Timestamp:   ts,
	}
	signMessage(&msg, priv)
	_ = encoder.Encode(msg)
}
