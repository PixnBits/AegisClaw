package runtime

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIsProjectManagerVM(t *testing.T) {
	if !isProjectManagerVM("project-manager", "project-manager-css-r1") {
		t.Fatal("type+id must match")
	}
	if !isProjectManagerVM("agent", "project-manager-main") {
		t.Fatal("id prefix must match")
	}
	if isProjectManagerVM("agent", "coder-css-r1") {
		t.Fatal("coder must not use the PM model")
	}
	if isProjectManagerVM("court-persona", "court-persona-ciso") {
		t.Fatal("court must not use the PM model")
	}
}

func TestWriteGitCIDKeyDecimal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git-cid-keys.json")
	t.Setenv("AEGIS_GIT_CID_KEYS", path)
	pub := make(ed25519.PublicKey, ed25519.PublicKeySize)
	pub[0] = 7
	writeGitCIDKey(dir, 3, pub)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["cid-3"]; ok {
		t.Fatal(`must not encode CID as "cid-3"`)
	}
	want := base64.StdEncoding.EncodeToString(pub)
	if m["3"] != want {
		t.Fatalf("key 3 = %q want %q (map=%v)", m["3"], want, m)
	}
}
