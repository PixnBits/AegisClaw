package runtime

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"AegisClaw/internal/config"
	"AegisClaw/internal/eventbus"
	"AegisClaw/internal/sandbox"
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

func TestDeleteGitCIDKeyDropsRow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git-cid-keys.json")
	t.Setenv("AEGIS_GIT_CID_KEYS", path)
	pub := make(ed25519.PublicKey, ed25519.PublicKeySize)
	pub[0] = 7
	writeGitCIDKey(dir, 3, pub)
	writeGitCIDKey(dir, 4, pub)
	want := base64.StdEncoding.EncodeToString(pub)
	deleteGitCIDKey(dir, 3, "other-pub")
	bKeep, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var keep map[string]string
	if err := json.Unmarshal(bKeep, &keep); err != nil {
		t.Fatal(err)
	}
	if keep["3"] != want {
		t.Fatalf("CAS mismatch must not delete CID 3: %v", keep)
	}
	deleteGitCIDKey(dir, 3, want)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["3"]; ok {
		t.Fatalf("CID 3 row must be deleted: %v", m)
	}
	if m["4"] != want {
		t.Fatalf("CID 4 must remain: %v", m)
	}
}

func TestStopVMCapturesCIDPubAndDeletesRow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git-cid-keys.json")
	t.Setenv("AEGIS_GIT_CID_KEYS", path)
	pub := make(ed25519.PublicKey, ed25519.PublicKeySize)
	pub[0] = 9
	writeGitCIDKey(dir, 42, pub)
	want := base64.StdEncoding.EncodeToString(pub)

	var gotCID uint32
	var gotPub string
	o := &Orchestrator{
		config:  &config.Config{StateDir: dir},
		backend: stubSandbox{},
		bus:     eventbus.New(),
		vms: map[string]*VMLifecycle{
			"vm-a": {
				ID: "vm-a",
				Config: sandbox.VMConfig{
					PublicKey:     pub,
					NetworkConfig: &sandbox.NetworkConfig{VsockPort: 42},
				},
			},
		},
		NotifyHubCIDUnlease: func(cid uint32, expectedPub string) {
			gotCID = cid
			gotPub = expectedPub
		},
	}
	if err := o.StopVM(context.Background(), "vm-a"); err != nil {
		t.Fatal(err)
	}
	if gotCID != 42 || gotPub != want {
		t.Fatalf("NotifyHubCIDUnlease cid=%d pub=%q want 42 %q", gotCID, gotPub, want)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["42"]; ok {
		t.Fatalf("StopVM must delete CID row: %s", b)
	}
}

func TestStopVMUnleasesAfterBackendStop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git-cid-keys.json")
	t.Setenv("AEGIS_GIT_CID_KEYS", path)
	pub := make(ed25519.PublicKey, ed25519.PublicKeySize)
	pub[0] = 3
	writeGitCIDKey(dir, 7, pub)
	want := base64.StdEncoding.EncodeToString(pub)

	var order []string
	o := &Orchestrator{
		config: &config.Config{StateDir: dir},
		backend: orderSandbox{onStop: func() {
			order = append(order, "stop")
			b, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("cid keys during Stop: %v", err)
				return
			}
			if !strings.Contains(string(b), `"7"`) {
				t.Errorf("CID row must still exist during backend.Stop (unlease after guest death): %s", b)
			}
		}},
		bus: eventbus.New(),
		vms: map[string]*VMLifecycle{
			"vm-a": {
				ID: "vm-a",
				Config: sandbox.VMConfig{
					PublicKey:     pub,
					NetworkConfig: &sandbox.NetworkConfig{VsockPort: 7},
				},
			},
		},
		NotifyHubCIDUnlease: func(uint32, string) {
			order = append(order, "unlease")
		},
	}
	if err := o.StopVM(context.Background(), "vm-a"); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "stop" || order[1] != "unlease" {
		t.Fatalf("order=%v want [stop unlease]", order)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), want) && strings.Contains(string(b), `"7"`) {
		var m map[string]string
		if json.Unmarshal(b, &m) != nil {
			t.Fatal(b)
		}
		if _, ok := m["7"]; ok {
			t.Fatalf("CID 7 row must be gone after unlease: %s", b)
		}
	}
}

type orderSandbox struct{ onStop func() }

func (orderSandbox) Start(context.Context, sandbox.VMConfig) error { return nil }
func (s orderSandbox) Stop(context.Context, string) error {
	if s.onStop != nil {
		s.onStop()
	}
	return nil
}
func (orderSandbox) Status(context.Context, string) (sandbox.Status, error) {
	return sandbox.StatusStopped, nil
}
func (orderSandbox) List(context.Context) ([]sandbox.VMInfo, error) { return nil, nil }
func (orderSandbox) Cleanup(context.Context) error                  { return nil }
func (orderSandbox) BootPhases(context.Context, string) map[string]int64 {
	return nil
}

type stubSandbox struct{}

func (stubSandbox) Start(context.Context, sandbox.VMConfig) error { return nil }
func (stubSandbox) Stop(context.Context, string) error            { return nil }
func (stubSandbox) Status(context.Context, string) (sandbox.Status, error) {
	return sandbox.StatusStopped, nil
}
func (stubSandbox) List(context.Context) ([]sandbox.VMInfo, error) { return nil, nil }
func (stubSandbox) Cleanup(context.Context) error                  { return nil }
func (stubSandbox) BootPhases(context.Context, string) map[string]int64 {
	return nil
}
