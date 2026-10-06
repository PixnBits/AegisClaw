package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestValidateVMID(t *testing.T) {
	valid := []string{
		"agent-ab12",
		"agent-1a2b",
		"memory-sess-1710000000000",
		"memory-deadbeef",
		"court-persona-ciso",
		"court-persona-security-architect",
		"court-persona-senior-coder",
		"court-persona-user-advocate",
		"court-scribe",
		"store",
		"network-boundary",
		"web-portal",
		"aegishub",
		"project-manager",
		"project-manager-main",
		"project-manager-css-r1",
		"coder-main",
		"coder-feature-1",
		"coder-plan-demo",
		"tester-feature-1",
		"builder-1",
		"builder-sit-1",
		"A",
		"web-portal.1",
		strings.Repeat("a", 128),
	}
	for _, id := range valid {
		if err := validateVMID(id); err != nil {
			t.Errorf("valid id %q rejected: %v", id, err)
		}
	}

	invalid := []string{
		"../x",
		"a/../../x",
		"/etc/x",
		"x\x00",
		"x\x00y",
		"",
		"..",
		".",
		"foo..bar",
		`a\b`,
		`..\x`,
		" has-space",
		"-leading-dash",
		strings.Repeat("a", 129),
	}
	for _, id := range invalid {
		if err := validateVMID(id); err == nil {
			t.Errorf("invalid id %q accepted", id)
		}
	}
}

func TestGatherVMLogs(t *testing.T) {
	stateDir := t.TempDir()
	outside := t.TempDir()
	secretPath := filepath.Join(outside, "secret.log")
	const secret = "TOP-SECRET-LOG-LINE\n"
	if err := os.WriteFile(secretPath, []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(stateDir, secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(rel, ".log") {
		t.Fatalf("relative path %q does not end in .log", rel)
	}
	traversalID := strings.TrimSuffix(rel, ".log")
	joined := filepath.Join(stateDir, traversalID+".log")
	if filepath.Clean(joined) != filepath.Clean(secretPath) {
		t.Fatalf("traversal id %q joined to %q, want %q", traversalID, joined, secretPath)
	}

	logs := gatherVMLogs(stateDir, traversalID, 50)
	if logs == nil {
		t.Fatal("invalid id returned nil map, want empty map")
	}
	if len(logs) != 0 {
		t.Fatalf("traversal id returned logs: %#v", logs)
	}
	for _, body := range logs {
		if strings.Contains(body, "TOP-SECRET") {
			t.Fatalf("traversal id leaked secret: %q", body)
		}
	}

	const id = "store"
	if err := os.WriteFile(filepath.Join(stateDir, "fc-"+id+".log"), []byte("vmm-ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "fc-"+id+"-console.log"), []byte("console-ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, id+".guest.log"), []byte("guest-ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, id+".log"), []byte("aux-ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := gatherVMLogs(stateDir, id, 20)
	want := map[string]string{
		"vmm":     "vmm-ok\n",
		"console": "console-ok\n",
		"guest":   "guest-ok\n",
		"log":     "aux-ok\n",
	}
	for key, body := range want {
		if got[key] != body {
			t.Errorf("valid id %s: got %q, want %q (all=%#v)", key, got[key], body, got)
		}
	}

	// A symlink at a valid log path must not be followed.
	linked := filepath.Join(outside, "linked.log")
	if err := os.WriteFile(linked, []byte("via-symlink\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(stateDir, "web-portal.log")
	if err := os.Symlink(linked, link); err != nil {
		t.Fatal(err)
	}
	sym := gatherVMLogs(stateDir, "web-portal", 20)
	if body, ok := sym["log"]; ok {
		t.Fatalf("symlink log was returned: %q", body)
	}
}

func TestVMLogsSocketRejectsTraversal(t *testing.T) {
	if cfg == nil {
		t.Fatal("cfg is nil")
	}
	stateDir := t.TempDir()
	outside := t.TempDir()
	secretPath := filepath.Join(outside, "secret.log")
	const secret = "SOCKET-SECRET-CANARY"
	if err := os.WriteFile(secretPath, []byte(secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(stateDir, secretPath)
	if err != nil {
		t.Fatal(err)
	}
	traversalID := strings.TrimSuffix(rel, ".log")
	if filepath.Clean(filepath.Join(stateDir, traversalID+".log")) != filepath.Clean(secretPath) {
		t.Fatalf("traversal id %q does not reach %s", traversalID, secretPath)
	}

	prev := cfg.StateDir
	cfg.StateDir = stateDir
	t.Cleanup(func() { cfg.StateDir = prev })

	resp := callVMLogs(t, traversalID)
	if resp.OK {
		t.Fatalf("traversal id accepted: %#v", resp)
	}
	if resp.Error == "" {
		t.Fatal("expected a clear error, got empty")
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("response leaked secret: %s", raw)
	}

	if err := os.WriteFile(filepath.Join(stateDir, "aegishub.log"), []byte("hub-line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	okResp := callVMLogs(t, "aegishub")
	if !okResp.OK {
		t.Fatalf("valid id rejected: %s", okResp.Error)
	}
	data, ok := okResp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("data type %T", okResp.Data)
	}
	logs, ok := data["logs"].(map[string]interface{})
	if !ok {
		t.Fatalf("logs type %T (%#v)", data["logs"], data)
	}
	if logs["log"] != "hub-line\n" {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestGetRecentFileContent(t *testing.T) {
	dir := t.TempDir()

	regular := filepath.Join(dir, "regular.log")
	const body = "alpha\nbeta\ngamma\n"
	if err := os.WriteFile(regular, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := getRecentFileContent(regular, 0); got != body {
		t.Fatalf("regular file: got %q, want %q", got, body)
	}
	if got := getRecentFileContent(regular, 2); got != "gamma\n" {
		t.Fatalf("tail: got %q, want %q", got, "gamma\n")
	}

	target := filepath.Join(dir, "target.log")
	const secret = "secret-via-symlink\n"
	if err := os.WriteFile(target, []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.log")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if got := readRecentWithTimeout(t, link, 50, 2*time.Second); got != "" {
		t.Fatalf("symlink returned %q, want empty (ELOOP)", got)
	}

	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := readRecentWithTimeout(t, sub, 50, 2*time.Second); got != "" {
		t.Fatalf("directory returned %q, want empty", got)
	}

	fifo := filepath.Join(dir, "fifo.log")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readRecentWithTimeout(t, fifo, 50, 2*time.Second); got != "" {
		t.Fatalf("fifo returned %q, want empty", got)
	}

	// /dev/null is a character device. A read is EOF, so "" alone does not
	// prove the regular-file check ran.
	nullInfo, err := os.Lstat("/dev/null")
	if err != nil {
		t.Fatalf("lstat /dev/null: %v", err)
	}
	if nullInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatal("/dev/null is a symlink; O_NOFOLLOW would hide the device check")
	}
	if nullInfo.Mode().IsRegular() {
		t.Fatal("/dev/null is a regular file; device case not exercised")
	}
	if got := readRecentWithTimeout(t, "/dev/null", 50, 2*time.Second); got != "" {
		t.Fatalf("/dev/null returned %q, want empty", got)
	}

	// An idle FIFO reads as EOF, so deleting the IsRegular check still
	// returns "". Hold the write end open: ReadAll then blocks.
	held := filepath.Join(dir, "fifo-held.log")
	if err := syscall.Mkfifo(held, 0o644); err != nil {
		t.Fatal(err)
	}
	hold, err := os.OpenFile(held, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hold.Close() })
	if got := readRecentWithTimeout(t, held, 50, 2*time.Second); got != "" {
		t.Fatalf("held fifo returned %q, want empty", got)
	}
}

func readRecentWithTimeout(t *testing.T, path string, tail int, d time.Duration) string {
	t.Helper()
	ch := make(chan string, 1)
	go func() {
		ch <- getRecentFileContent(path, tail)
	}()
	select {
	case got := <-ch:
		return got
	case <-time.After(d):
		t.Fatalf("getRecentFileContent(%s) blocked for %s", path, d)
	}
	return ""
}

func callVMLogs(t *testing.T, id string) SocketResponse {
	t.Helper()
	body, err := json.Marshal(SocketRequest{Op: "vm.logs", Args: map[string]string{"id": id}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	done := make(chan struct{})
	go func() {
		handleSocketCommand(server, nil)
		close(done)
	}()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := client.Write(body); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 4096)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var resp SocketResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		t.Fatalf("unmarshal %q: %v", buf[:n], err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not return")
	}
	return resp
}
