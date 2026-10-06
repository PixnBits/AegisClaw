package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestGuestBridgeRegisterAllowed(t *testing.T) {
	tests := []struct {
		vmID   string
		source string
		ok     bool
	}{
		{vmID: "coder-1", source: "coder-1", ok: true},
		{vmID: "coder-1", source: "store", ok: false},
		{vmID: "coder-1", source: "court-persona-ciso", ok: false},
		{vmID: "store", source: "store", ok: true},
		{vmID: "court-persona-ciso", source: "court-persona-ciso", ok: true},
		{vmID: "court-scribe", source: "court-scribe", ok: true},
		{vmID: "daemon-orchestrator", source: "daemon-orchestrator", ok: false},
		{vmID: "daemon", source: "daemon", ok: false},
		{vmID: "daemon-temp-1", source: "daemon-temp-1", ok: false},
		{vmID: "daemon-internal", source: "daemon-internal", ok: false},
		{vmID: "channel-facilitator-out-1", source: "channel-facilitator-out-1", ok: false},
		{vmID: "channel-facilitator", source: "channel-facilitator", ok: false},
		{vmID: "hub", source: "hub", ok: false},
		{vmID: "hub-perm-fetch", source: "hub-perm-fetch", ok: false},
		{vmID: "hub-perm-fetch-1", source: "hub-perm-fetch-1", ok: false},
		{vmID: "aegis-cli-internal", source: "aegis-cli-internal", ok: false},
		{vmID: "aegis-cli-internal-1", source: "aegis-cli-internal-1", ok: false},
		{vmID: "", source: "", ok: false},
		{vmID: "coder-1", source: "", ok: false},
		{vmID: "agent-s1", source: "agent-s2", ok: false},
		// Hyphen boundary: these are not the host-only prefixes.
		{vmID: "hub-perm-fetcher", source: "hub-perm-fetcher", ok: true},
		{vmID: "daemonfoo", source: "daemonfoo", ok: true},
	}
	for _, tc := range tests {
		name := tc.vmID + "/" + tc.source
		if tc.vmID == "" && tc.source == "" {
			name = "empty/empty"
		}
		t.Run(name, func(t *testing.T) {
			ok, reason := guestBridgeRegisterAllowed(tc.vmID, tc.source)
			if ok != tc.ok {
				t.Fatalf("guestBridgeRegisterAllowed(%q, %q) = %v (%q), want %v", tc.vmID, tc.source, ok, reason, tc.ok)
			}
			if ok && reason != "" {
				t.Fatalf("allowed registration returned reason %q", reason)
			}
			if !ok && reason == "" {
				t.Fatal("refusal returned an empty reason")
			}
		})
	}
}

func TestBridgeGuestConnRefuses(t *testing.T) {
	var logs bytes.Buffer
	logrus.SetOutput(&logs)
	t.Cleanup(func() { logrus.SetOutput(os.Stderr) })

	t.Run("impersonate store", func(t *testing.T) {
		logs.Reset()
		line := guestBridgeJSONLine(t, map[string]any{
			"source":      "store",
			"destination": "hub",
			"command":     "register",
			"payload":     map[string]string{"public_key": "not-the-store-key", "version": "test"},
			"timestamp":   "2026-10-05T00:00:00Z",
			"signature":   "dummy",
		})
		dials := assertGuestBridgeRefused(t, "coder-1", line)
		if dials != 0 {
			t.Fatalf("dial count = %d, want 0", dials)
		}
		got := logs.String()
		if !strings.Contains(got, `Audit: guest hub bridge coder-1 refused register as "store"`) &&
			!strings.Contains(got, `Audit: guest hub bridge coder-1 refused register as \"store\"`) {
			t.Fatalf("audit log missing, got %q", got)
		}
		if !strings.Contains(got, "source does not match vm id") {
			t.Fatalf("audit reason missing, got %q", got)
		}
	})

	t.Run("non-register", func(t *testing.T) {
		line := guestBridgeJSONLine(t, map[string]any{
			"source":      "coder-1",
			"destination": "hub",
			"command":     "ping",
			"payload":     map[string]string{},
			"timestamp":   "2026-10-05T00:00:00Z",
			"signature":   "",
		})
		if dials := assertGuestBridgeRefused(t, "coder-1", line); dials != 0 {
			t.Fatalf("dial count = %d, want 0", dials)
		}
	})

	t.Run("destination", func(t *testing.T) {
		line := guestBridgeJSONLine(t, map[string]any{
			"source":      "coder-1",
			"destination": "store",
			"command":     "register",
			"payload":     map[string]string{},
			"timestamp":   "2026-10-05T00:00:00Z",
			"signature":   "",
		})
		if dials := assertGuestBridgeRefused(t, "coder-1", line); dials != 0 {
			t.Fatalf("dial count = %d, want 0", dials)
		}
	})

	t.Run("oversized", func(t *testing.T) {
		line := bytes.Repeat([]byte("A"), guestBridgeMaxRegisterLine+64)
		if dials := assertGuestBridgeRefused(t, "coder-1", line); dials != 0 {
			t.Fatalf("dial count = %d, want 0", dials)
		}
	})
}

func TestBridgeGuestConnRegisterReadDeadline(t *testing.T) {
	prev := guestBridgeRegisterReadTimeout
	guestBridgeRegisterReadTimeout = 200 * time.Millisecond
	t.Cleanup(func() { guestBridgeRegisterReadTimeout = prev })

	start := time.Now()
	dials := assertGuestBridgeRefused(t, "coder-1", nil)
	elapsed := time.Since(start)
	if dials != 0 {
		t.Fatalf("dial count = %d, want 0", dials)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("read returned in %s, want the deadline to apply", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("read blocked for %s, want about %s", elapsed, guestBridgeRegisterReadTimeout)
	}
}

func TestBridgeGuestConnForwardsRegisterAndBufferedBytes(t *testing.T) {
	_, sock := shortUnixSock(t, "aegis-br-")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	type gotLines struct {
		first  []byte
		second []byte
	}
	gotCh := make(chan gotLines, 1)
	errHub := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errHub <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		br := bufio.NewReader(conn)
		first, err := br.ReadBytes('\n')
		if err != nil {
			errHub <- err
			return
		}
		if _, err := io.WriteString(conn, "{\"status\":\"registered\",\"assigned_id\":\"coder-1\"}\n"); err != nil {
			errHub <- err
			return
		}
		second, err := br.ReadBytes('\n')
		if err != nil {
			errHub <- err
			return
		}
		gotCh <- gotLines{first: append([]byte(nil), first...), second: append([]byte(nil), second...)}
	}()

	reg := guestBridgeJSONLine(t, map[string]any{
		"source":      "coder-1",
		"destination": "hub",
		"command":     "register",
		"payload":     map[string]string{"public_key": "test", "version": "test"},
		"timestamp":   "2026-10-05T00:00:00Z",
		"signature":   "dummy",
	})
	extra := []byte("{\"source\":\"coder-1\",\"destination\":\"hub\",\"command\":\"ping\"}\n")
	payload := append(append([]byte(nil), reg...), extra...)

	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	var dials atomic.Int32
	errCh := make(chan error, 1)
	go func() {
		errCh <- bridgeGuestConn("coder-1", server, func() (net.Conn, error) {
			dials.Add(1)
			return net.Dial("unix", sock)
		})
	}()
	go func() {
		_, _ = client.Write(payload)
	}()

	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply, err := bufio.NewReader(client).ReadBytes('\n')
	if err != nil {
		t.Fatalf("guest read: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(reply, &resp); err != nil {
		t.Fatalf("reply %q: %v", reply, err)
	}
	if resp["status"] != "registered" || resp["assigned_id"] != "coder-1" {
		t.Fatalf("reply %#v", resp)
	}

	select {
	case err := <-errHub:
		t.Fatalf("fake hub: %v", err)
	case got := <-gotCh:
		if string(got.first) != string(reg) {
			t.Fatalf("first line\n got %q\nwant %q", got.first, reg)
		}
		if string(got.second) != string(extra) {
			t.Fatalf("buffered line\n got %q\nwant %q", got.second, extra)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fake hub timed out")
	}
	if dials.Load() != 1 {
		t.Fatalf("dial count = %d, want 1", dials.Load())
	}

	_ = client.Close()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge did not finish")
	}
}

// TestGuestHubBridgeStoreSurvives is the phase-1 proof against a real hub:
// a coder VM that registers as store never dials, and the real store connection
// stays usable. A register as coder-1 is forwarded and gets status=registered.
func TestGuestHubBridgeStoreSurvives(t *testing.T) {
	root := repoRoot(t)
	aclPath := filepath.Join(root, "config", "acls.yaml")
	if _, err := os.Stat(aclPath); err != nil {
		t.Fatalf("acl file: %v", err)
	}
	bin := buildAegisHubTestBinary(t, root)
	_, sock := shortUnixSock(t, "aegis-ghb-")

	var hubLog bytes.Buffer
	hubCmd := exec.Command(bin, "start")
	hubCmd.Dir = filepath.Dir(sock)
	hubCmd.Env = hubTestEnv(sock, aclPath)
	hubCmd.Stdout = &hubLog
	hubCmd.Stderr = &hubLog
	if err := hubCmd.Start(); err != nil {
		t.Fatalf("start hub: %v", err)
	}
	t.Cleanup(func() {
		if t.Failed() && hubLog.Len() > 0 {
			t.Logf("hub output:\n%s", hubLog.String())
		}
	})
	t.Cleanup(func() {
		if hubCmd.Process != nil {
			_ = hubCmd.Process.Kill()
			_, _ = hubCmd.Process.Wait()
		}
	})
	waitForUnixSocket(t, sock, 10*time.Second)

	storePub, storePriv := newTestKey(t)
	storeLine := signedHubLine(t, "store", "register", map[string]string{
		"public_key": base64.StdEncoding.EncodeToString(storePub),
		"version":    "test",
	}, storePriv)

	storeClient, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial store: %v", err)
	}
	t.Cleanup(func() { _ = storeClient.Close() })
	if _, err := storeClient.Write(storeLine); err != nil {
		t.Fatalf("write store register: %v", err)
	}
	storeBR := bufio.NewReader(storeClient)
	_ = storeClient.SetReadDeadline(time.Now().Add(5 * time.Second))
	storeResp := mustReadJSONLine(t, storeBR)
	_ = storeClient.SetReadDeadline(time.Time{})
	if storeResp["status"] != "registered" || storeResp["assigned_id"] != "store" {
		t.Fatalf("store register response: %#v", storeResp)
	}

	forgedPub, forgedPriv := newTestKey(t)
	forged := signedHubLine(t, "store", "register", map[string]string{
		"public_key": base64.StdEncoding.EncodeToString(forgedPub),
		"version":    "test",
	}, forgedPriv)
	var dials atomic.Int32
	dial := func() (net.Conn, error) {
		dials.Add(1)
		return net.Dial("unix", sock)
	}
	if err := bridgeRegisterFromGuest(t, "coder-1", forged, dial); err == nil {
		t.Fatal("forged store register was forwarded")
	}
	if dials.Load() != 0 {
		t.Fatalf("forged register dial count = %d, want 0", dials.Load())
	}

	_ = storeClient.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	var idle [1]byte
	_, idleErr := storeBR.Read(idle[:])
	if idleErr == nil {
		t.Fatalf("store conn received unexpected data after forged register: %q", idle)
	}
	if !errors.Is(idleErr, os.ErrDeadlineExceeded) {
		t.Fatalf("store conn closed after forged register: %v", idleErr)
	}
	_ = storeClient.SetDeadline(time.Now().Add(3 * time.Second))
	versionLine := signedHubLine(t, "store", "get-version", map[string]string{"echo": "store-alive"}, storePriv)
	if _, err := storeClient.Write(versionLine); err != nil {
		t.Fatalf("write get-version: %v", err)
	}
	versionResp := mustReadJSONLine(t, storeBR)
	if versionResp["status"] != "ok" {
		t.Fatalf("store get-version = %#v, want status ok (original key still registered)", versionResp)
	}
	_ = storeClient.SetDeadline(time.Time{})

	coderPub, coderPriv := newTestKey(t)
	coderLine := signedHubLine(t, "coder-1", "register", map[string]string{
		"public_key": base64.StdEncoding.EncodeToString(coderPub),
		"version":    "test",
	}, coderPriv)
	reply, err := bridgeRegisterFromGuestOK(t, "coder-1", coderLine, dial)
	if err != nil {
		t.Fatal(err)
	}
	if reply["status"] != "registered" || reply["assigned_id"] != "coder-1" {
		t.Fatalf("coder-1 register reply: %#v", reply)
	}
	if dials.Load() != 1 {
		t.Fatalf("successful register dial count = %d, want 1", dials.Load())
	}
}

func assertGuestBridgeRefused(t *testing.T, vmID string, line []byte) int32 {
	t.Helper()
	var dials atomic.Int32
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	errCh := make(chan error, 1)
	go func() {
		errCh <- bridgeGuestConn(vmID, server, func() (net.Conn, error) {
			dials.Add(1)
			return nil, errors.New("hub dial should not be called")
		})
	}()
	if line != nil {
		go func() {
			_, _ = client.Write(line)
		}()
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected refusal")
		}
	case <-timer.C:
		t.Fatal("bridgeGuestConn timed out")
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	var buf [1]byte
	_, rerr := client.Read(buf[:])
	if rerr == nil {
		t.Fatal("expected guest side closed")
	}
	if !errors.Is(rerr, io.EOF) && !errors.Is(rerr, io.ErrClosedPipe) && !errors.Is(rerr, net.ErrClosed) {
		t.Fatalf("guest side error = %v, want EOF/closed", rerr)
	}
	return dials.Load()
}

func bridgeRegisterFromGuest(t *testing.T, vmID string, line []byte, dial func() (net.Conn, error)) error {
	t.Helper()
	client, server := net.Pipe()
	defer client.Close()
	errCh := make(chan error, 1)
	go func() {
		errCh <- bridgeGuestConn(vmID, server, dial)
	}()
	go func() {
		_, _ = client.Write(line)
	}()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	var err error
	select {
	case err = <-errCh:
	case <-timer.C:
		t.Fatal("bridgeGuestConn timed out")
	}
	if err == nil {
		return nil
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	var buf [1]byte
	_, rerr := client.Read(buf[:])
	if rerr == nil || (!errors.Is(rerr, io.EOF) && !errors.Is(rerr, io.ErrClosedPipe) && !errors.Is(rerr, net.ErrClosed)) {
		t.Fatalf("guest side after refusal: %v", rerr)
	}
	return err
}

func bridgeRegisterFromGuestOK(t *testing.T, vmID string, line []byte, dial func() (net.Conn, error)) (map[string]any, error) {
	t.Helper()
	client, server := net.Pipe()
	defer client.Close()
	errCh := make(chan error, 1)
	go func() {
		errCh <- bridgeGuestConn(vmID, server, dial)
	}()
	go func() {
		_, _ = client.Write(line)
	}()
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(client)
	resp := mustReadJSONLine(t, br)
	_ = client.Close()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case err := <-errCh:
		return resp, err
	case <-timer.C:
		t.Fatal("bridge did not finish after guest close")
	}
	return resp, nil
}

func mustReadJSONLine(t *testing.T, br *bufio.Reader) map[string]any {
	t.Helper()
	line, err := br.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read json line: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("json %q: %v", line, err)
	}
	return resp
}

func guestBridgeJSONLine(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

type guestBridgeTestWire struct {
	Source      string          `json:"source"`
	Destination string          `json:"destination"`
	Command     string          `json:"command"`
	Payload     json.RawMessage `json:"payload"`
	Timestamp   string          `json:"timestamp"`
	Signature   string          `json:"signature"`
}

func signedHubLine(t *testing.T, source, command string, payload any, priv ed25519.PrivateKey) []byte {
	t.Helper()
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	w := guestBridgeTestWire{
		Source:      source,
		Destination: "hub",
		Command:     command,
		Payload:     rawPayload,
		Timestamp:   "2026-10-05T00:00:00Z",
	}
	unsigned, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	w.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, unsigned))
	signed, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	return append(signed, '\n')
}

func newTestKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func shortUnixSock(t *testing.T, pattern string) (string, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", pattern)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir, filepath.Join(dir, "h.sock")
}

func buildAegisHubTestBinary(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "aegishub")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/aegishub")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build aegishub: %v\n%s", err, out)
	}
	return bin
}

func hubTestEnv(sock, aclPath string) []string {
	drop := map[string]bool{
		"AEGIS_HUB_SOCKET":       true,
		"AEGIS_DEV_MODE":         true,
		"AEGIS_ACL_FILE":         true,
		"AEGIS_GIT_IDENTITIES":   true,
		"AEGIS_GIT_CID_KEYS":     true,
		"AEGIS_STORE_GIT_SOCKET": true,
	}
	env := make([]string, 0, len(os.Environ())+3)
	for _, e := range os.Environ() {
		name, _, _ := strings.Cut(e, "=")
		if drop[name] {
			continue
		}
		env = append(env, e)
	}
	return append(env,
		"AEGIS_HUB_SOCKET="+sock,
		"AEGIS_DEV_MODE=1",
		"AEGIS_ACL_FILE="+aclPath,
	)
}
