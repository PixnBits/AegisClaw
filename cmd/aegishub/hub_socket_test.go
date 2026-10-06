package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"AegisClaw/internal/hublease"

	"github.com/mdlayher/vsock"
)

func snapshotHubRegistry(t *testing.T) {
	t.Helper()
	registeredMutex.Lock()
	prev := make(map[string]*RegisteredComponent, len(registered))
	for k, v := range registered {
		prev[k] = v
	}
	registeredMutex.Unlock()
	tempConnMutex.Lock()
	prevN := tempConnCounter
	tempConnMutex.Unlock()
	t.Cleanup(func() {
		registeredMutex.Lock()
		for k := range registered {
			if _, ok := prev[k]; !ok {
				delete(registered, k)
			}
		}
		for k, v := range prev {
			registered[k] = v
		}
		registeredMutex.Unlock()
		tempConnMutex.Lock()
		tempConnCounter = prevN
		tempConnMutex.Unlock()
	})
}

func waitHandler(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handleConnection did not return")
	}
}

func TestAuthorizeHubPeer(t *testing.T) {
	const self = 1000
	const original = 2000
	cases := []struct {
		name        string
		peerUID     int
		peerOK      bool
		selfUID     int
		originalUID int
		want        bool
	}{
		{name: "root allowed", peerUID: 0, peerOK: true, selfUID: self, originalUID: original, want: true},
		{name: "self allowed", peerUID: self, peerOK: true, selfUID: self, originalUID: original, want: true},
		{name: "original user allowed", peerUID: original, peerOK: true, selfUID: 0, originalUID: original, want: true},
		{name: "other uid denied", peerUID: 3000, peerOK: true, selfUID: self, originalUID: original, want: false},
		{name: "missing peer creds denied", peerUID: 0, peerOK: false, selfUID: 0, originalUID: 0, want: false},
		{name: "missing creds deny even if uid matches self", peerUID: self, peerOK: false, selfUID: self, originalUID: self, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := authorizeHubPeer(tc.peerUID, tc.peerOK, tc.selfUID, tc.originalUID)
			if got != tc.want {
				t.Fatalf("authorizeHubPeer(%d, %v, %d, %d) = %v, want %v", tc.peerUID, tc.peerOK, tc.selfUID, tc.originalUID, got, tc.want)
			}
		})
	}
}

func TestListenHubUnixSocketModeAndPeer(t *testing.T) {
	t.Setenv("SUDO_USER", "")
	snapshotHubRegistry(t)
	// umask is process-wide. Do not call t.Parallel.
	origUmask := syscall.Umask(0)
	t.Cleanup(func() { _ = syscall.Umask(origUmask) })

	sock := filepath.Join(t.TempDir(), "hub.sock")
	ln, err := listenHubUnixSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	fi, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("hub socket mode = %o, want 0600 (umask was 0)", fi.Mode().Perm())
	}
	if got := syscall.Umask(origUmask); got != 0 {
		t.Fatalf("umask = %#o after listenHubUnixSocket, want 0 restored", got)
	}

	accepted := make(chan net.Conn, 1)
	errc := make(chan error, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			errc <- aerr
			return
		}
		accepted <- c
	}()
	client, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var server net.Conn
	select {
	case err := <-errc:
		t.Fatal(err)
	case server = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("accept timed out")
	}
	defer server.Close()

	peerUID, peerOK := getHubPeerUID(server)
	if !peerOK {
		t.Fatal("getHubPeerUID failed on a real unix connection")
	}
	self := os.Geteuid()
	if peerUID != self {
		t.Fatalf("peer uid %d, want euid %d", peerUID, self)
	}
	if !authorizeHubPeer(peerUID, peerOK, self, self) {
		t.Fatal("authorizeHubPeer denied the dialing process")
	}
	_ = server.Close()
	_ = client.Close()

	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go handleConnection(c, &sync.Map{})
		}
	}()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	reg := Message{
		Source:      "peer-ok-client",
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": base64.StdEncoding.EncodeToString(pub), "version": "test"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := json.NewEncoder(conn).Encode(reg); err != nil {
		t.Fatal(err)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("register decode: %v", err)
	}
	if errVal, ok := resp["error"]; ok {
		t.Fatalf("unix peer from this process rejected: %v", errVal)
	}
	if resp["status"] != "registered" {
		t.Fatalf("register response: %#v", resp)
	}
}

// TestHandleConnectionUnixPeerGate drives the real handleConnection on an
// accepted *net.UnixConn. authorizeHubPeer alone does not prove that deleting
// the peer check still rejects the connection.
func TestHandleConnectionUnixPeerGate(t *testing.T) {
	snapshotHubRegistry(t)
	t.Setenv("SUDO_USER", "")

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	self := os.Geteuid()
	original := hubOriginalUID()
	const foreign = 4242
	if authorizeHubPeer(foreign, true, self, original) {
		t.Fatalf("uid %d is allowed for self=%d original=%d", foreign, self, original)
	}
	if !authorizeHubPeer(self, true, self, original) {
		t.Fatalf("self uid %d is not allowed (original=%d)", self, original)
	}

	t.Run("foreign uid", func(t *testing.T) {
		prev := hubPeerUIDLookup
		hubPeerUIDLookup = func(net.Conn) (int, bool) { return foreign, true }
		t.Cleanup(func() { hubPeerUIDLookup = prev })
		assertUnixPeerRejected(t, "unix-peer-foreign", pub)
	})

	t.Run("lookup not ok", func(t *testing.T) {
		// uid 0 would be allowed if a missing creds result failed open.
		prev := hubPeerUIDLookup
		hubPeerUIDLookup = func(net.Conn) (int, bool) { return 0, false }
		t.Cleanup(func() { hubPeerUIDLookup = prev })
		assertUnixPeerRejected(t, "unix-peer-nocreds", pub)
	})

	t.Run("real peer registers", func(t *testing.T) {
		prev := hubPeerUIDLookup
		hubPeerUIDLookup = getHubPeerUID
		t.Cleanup(func() { hubPeerUIDLookup = prev })
		assertUnixPeerRegistered(t, "unix-peer-real", pub)
	})
}

func assertUnixPeerRejected(t *testing.T, id string, pub ed25519.PublicKey) {
	t.Helper()
	client, _, conns, done := startUnixHubConn(t, id)
	writeErr := writeHubRegister(client, id, pub)
	raw, err := readHubLine(client)
	if err != nil {
		t.Fatalf("read response: %v (register write: %v)", err, writeErr)
	}
	if raw != `{"error":"ERR_UNAUTHORIZED_PEER"}` {
		t.Fatalf("response = %s, want {\"error\":\"ERR_UNAUTHORIZED_PEER\"}", raw)
	}
	if _, err := readHubLine(client); !peerConnClosed(err) {
		t.Fatalf("connection not closed after unauthorized peer: %v", err)
	}
	waitHandler(t, done)
	if present, where := hubIDRegistered(id, conns); present {
		t.Fatalf("unauthorized peer registered in %s", where)
	}
}

func assertUnixPeerRegistered(t *testing.T, id string, pub ed25519.PublicKey) {
	t.Helper()
	client, server, conns, done := startUnixHubConn(t, id)
	if err := writeHubRegister(client, id, pub); err != nil {
		t.Fatalf("register write: %v", err)
	}
	raw, err := readHubLine(client)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	if errVal, ok := resp["error"]; ok {
		t.Fatalf("real unix peer rejected: %v", errVal)
	}
	if resp["status"] != "registered" || resp["assigned_id"] != id {
		t.Fatalf("register response: %s", raw)
	}
	got, loaded := conns.Load(id)
	if !loaded || got != server {
		t.Fatalf("conns[%s] = %#v, want the accepted conn", id, got)
	}
	registeredMutex.RLock()
	reg := registered[id]
	registeredMutex.RUnlock()
	if reg == nil || reg.ID != id {
		t.Fatalf("registered[%s] = %#v", id, reg)
	}
	_ = client.Close()
	waitHandler(t, done)
}

func startUnixHubConn(t *testing.T, id string) (client net.Conn, server *net.UnixConn, conns *sync.Map, done <-chan struct{}) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), id+".sock")
	ln, err := listenHubUnixSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	accErr := make(chan error, 1)
	accC := make(chan net.Conn, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			accErr <- aerr
			return
		}
		accC <- c
	}()
	client, err = net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var accepted net.Conn
	select {
	case err := <-accErr:
		t.Fatal(err)
	case accepted = <-accC:
	case <-time.After(3 * time.Second):
		t.Fatal("accept timed out")
	}
	var ok bool
	server, ok = accepted.(*net.UnixConn)
	if !ok {
		t.Fatalf("accepted %T, want *net.UnixConn", accepted)
	}

	conns = &sync.Map{}
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		handleConnection(server, conns)
	}()
	return client, server, conns, doneCh
}

func writeHubRegister(client net.Conn, id string, pub ed25519.PublicKey) error {
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	reg := Message{
		Source:      id,
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": base64.StdEncoding.EncodeToString(pub), "version": "test"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	return json.NewEncoder(client).Encode(reg)
}

func readHubLine(client net.Conn) (string, error) {
	line, err := bufio.NewReader(client).ReadBytes('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(line)), nil
}

func peerConnClosed(err error) bool {
	// The rejected handler closes without reading the register line, so the
	// follow-up read is EOF or ECONNRESET (the write hit a closed peer).
	return err != nil && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE))
}

func hubIDRegistered(id string, conns *sync.Map) (bool, string) {
	if _, ok := conns.Load(id); ok {
		return true, "conns"
	}
	registeredMutex.RLock()
	_, present := registered[id]
	registeredMutex.RUnlock()
	if present {
		return true, "registered"
	}
	return false, ""
}

func TestReservedIDReason(t *testing.T) {
	cases := []struct {
		id   string
		vm   bool
		want bool
	}{
		{id: "hub", vm: false, want: true},
		{id: "hub", vm: true, want: true},
		{id: "hub-perm-fetch", vm: false, want: true},
		{id: "hub-perm-fetch-9", vm: true, want: true},
		{id: "hub-perm-fetcher", vm: true, want: false},
		{id: "store", vm: true, want: true},
		{id: "store", vm: false, want: false},
		{id: "daemon-internal-1", vm: true, want: true},
		{id: "daemon-internal-1", vm: false, want: false},
		{id: "channel-facilitator-out-1", vm: true, want: true},
		{id: "channel-facilitator-out-1", vm: false, want: false},
		{id: "daemon", vm: true, want: true},
		{id: "daemon", vm: false, want: false},
		{id: "daemon-temp-3", vm: true, want: true},
		{id: "daemon-orchestrator", vm: true, want: true},
		{id: "aegis-cli-internal", vm: true, want: true},
		{id: "aegis-cli-internal-2", vm: true, want: true},
		{id: "channel-facilitator", vm: true, want: true},
		{id: "web-portal", vm: true, want: false},
		{id: "coder-1", vm: true, want: false},
		{id: "agent-1", vm: true, want: false},
		{id: "memory-1", vm: true, want: false},
		{id: "project-manager-1", vm: true, want: false},
		{id: "court-persona-ciso", vm: true, want: false},
	}
	for _, tc := range cases {
		reason, got := reservedIDReason(tc.id, tc.vm)
		if got != tc.want {
			t.Errorf("reservedIDReason(%q, vm=%v) = %q, %v; want reserved=%v", tc.id, tc.vm, reason, got, tc.want)
		}
		if got && reason == "" {
			t.Errorf("reservedIDReason(%q, vm=%v) reserved with empty reason", tc.id, tc.vm)
		}
	}
}

func TestHubIDRegistrationRejected(t *testing.T) {
	snapshotHubRegistry(t)
	const id = "hub"
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hub, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConnection(hub, &sync.Map{})
	}()
	reg := Message{
		Source:      id,
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": base64.StdEncoding.EncodeToString(pub), "version": "1"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(client).Encode(reg); err != nil {
		t.Fatal(err)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(client).Decode(&resp); err != nil {
		t.Fatalf("register decode: %v", err)
	}
	errVal, _ := resp["error"].(string)
	if !strings.Contains(errVal, "ERR_RESERVED_ID") {
		t.Fatalf("reply = %#v, want ERR_RESERVED_ID", resp)
	}
	if strings.Contains(errVal, "ERR_UNAUTHORIZED_PEER") {
		t.Fatal("net.Pipe must not be subject to the unix peer check")
	}
	_ = client.Close()
	waitHandler(t, done)
	registeredMutex.RLock()
	_, present := registered[id]
	registeredMutex.RUnlock()
	if present {
		t.Fatalf("registered[%q] must be absent", id)
	}
}

func signRegisterSource(priv ed25519.PrivateKey, source, pub string) Message {
	msg := Message{
		Source:      source,
		Destination: "hub",
		Command:     "register",
		Payload:     map[string]string{"public_key": pub, "version": "1"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(msg)
	msg.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body))
	return msg
}

func TestReservedVsockRegisterDoesNotFillCIDLease(t *testing.T) {
	resetCIDLeases()
	t.Cleanup(resetCIDLeases)
	snapshotHubRegistry(t)

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubStr := base64.StdEncoding.EncodeToString(pub)
	dir := t.TempDir()
	identPath := filepath.Join(dir, "git-identities.json")
	identJSON, err := json.Marshal(map[string]string{pubStr: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identPath, identJSON, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_GIT_IDENTITIES", identPath)

	storeAddr := &vsock.Addr{ContextID: 42, Port: 9999}
	resp := guestVsockHandshake(t, storeAddr, signRegisterSource(priv, "store", pubStr))
	errVal, _ := resp["error"].(string)
	if !strings.Contains(errVal, "ERR_RESERVED_ID") {
		t.Fatalf("store on vsock reply = %#v, want ERR_RESERVED_ID", resp)
	}
	if _, ok := hublease.LoadLease(42); ok {
		t.Fatal("reserved vsock register filled a CID lease")
	}

	hubAddr := &vsock.Addr{ContextID: 44, Port: 9999}
	resp = guestVsockHandshake(t, hubAddr, signRegisterSource(priv, "hub", pubStr))
	errVal, _ = resp["error"].(string)
	if !strings.Contains(errVal, "ERR_RESERVED_ID") {
		t.Fatalf("hub on vsock reply = %#v, want ERR_RESERVED_ID", resp)
	}
	if _, ok := hublease.LoadLease(44); ok {
		t.Fatal("reserved hub id on vsock filled a CID lease")
	}

	// Positive control: the same rostered signature fills a lease for a guest id.
	coderAddr := &vsock.Addr{ContextID: 43, Port: 9999}
	resp = guestVsockHandshake(t, coderAddr, signRegisterSource(priv, "coder-1", pubStr))
	if _, ok := resp["error"]; ok {
		t.Fatalf("coder-1 on vsock must be allowed, got %#v", resp)
	}
	if resp["status"] != "registered" {
		t.Fatalf("coder-1 register response: %#v", resp)
	}
	if got, ok := hublease.LoadLease(43); !ok || got != pubStr {
		t.Fatalf("coder-1 handshake should still fill the CID lease: got %q ok=%v", got, ok)
	}

	portalAddr := &vsock.Addr{ContextID: 45, Port: 9999}
	resp = guestVsockHandshake(t, portalAddr, signRegisterSource(priv, "web-portal", pubStr))
	if errVal, ok := resp["error"].(string); ok && strings.Contains(errVal, "ERR_RESERVED_ID") {
		t.Fatalf("web-portal on vsock must not be reserved: %#v", resp)
	}
	if resp["status"] != "registered" {
		t.Fatalf("web-portal register response: %#v", resp)
	}
}

func TestReregisterClosesPreviousConnection(t *testing.T) {
	snapshotHubRegistry(t)
	t.Setenv("AEGIS_DEV_MODE", "1")
	const id = "reregister-client"
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	conns := &sync.Map{}
	client1, _, done1 := registerTestComponent(t, conns, id, pub)
	first, ok := conns.Load(id)
	if !ok || first == nil {
		t.Fatal("conns missing first connection")
	}
	client2, _, done2 := registerTestComponent(t, conns, id, pub)
	t.Cleanup(func() {
		_ = client1.Close()
		_ = client2.Close()
	})
	second, ok := conns.Load(id)
	if !ok || second == nil || second == first {
		t.Fatal("conns does not hold the second connection")
	}

	_ = client1.SetReadDeadline(time.Now().Add(time.Second))
	var buf [1]byte
	_, err = client1.Read(buf[:])
	if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("first connection still open: %v", err)
	}
	waitHandler(t, done1)

	registeredMutex.RLock()
	got := registered[id]
	registeredMutex.RUnlock()
	if got == nil {
		t.Fatal("second registration was removed when the first connection closed")
	}
	still, ok := conns.Load(id)
	if !ok || still != second {
		t.Fatal("conns lost the second connection after the first closed")
	}

	_ = client2.Close()
	waitHandler(t, done2)
}

func TestDaemonTempDoesNotClosePersistentDaemon(t *testing.T) {
	snapshotHubRegistry(t)
	t.Setenv("AEGIS_DEV_MODE", "1")
	registeredMutex.Lock()
	delete(registered, "daemon")
	registeredMutex.Unlock()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	conns := &sync.Map{}
	daemonClient, _, daemonDone := registerTestComponent(t, conns, "daemon", pub)
	t.Cleanup(func() { _ = daemonClient.Close() })
	persistent, ok := conns.Load("daemon")
	if !ok || persistent == nil {
		t.Fatal("conns missing persistent daemon")
	}
	registeredMutex.RLock()
	persistentReg := registered["daemon"]
	registeredMutex.RUnlock()
	if persistentReg == nil {
		t.Fatal("persistent daemon registration missing")
	}

	hub, tempClient := net.Pipe()
	tempDone := make(chan struct{})
	go func() {
		defer close(tempDone)
		handleConnection(hub, conns)
	}()
	t.Cleanup(func() { _ = tempClient.Close() })
	reg := Message{
		Source:      "daemon",
		Destination: "hub",
		Command:     "register",
		Payload: map[string]string{
			"public_key": base64.StdEncoding.EncodeToString(pub),
			"version":    "test",
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Signature: "dummy",
	}
	_ = tempClient.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(tempClient).Encode(reg); err != nil {
		t.Fatal(err)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(tempClient).Decode(&resp); err != nil {
		t.Fatalf("daemon-temp register decode: %v", err)
	}
	if errVal, ok := resp["error"]; ok {
		t.Fatalf("daemon-temp register error: %v", errVal)
	}
	assigned, _ := resp["assigned_id"].(string)
	if !strings.HasPrefix(assigned, "daemon-temp-") {
		t.Fatalf("assigned_id = %q, want daemon-temp-*", assigned)
	}

	still, ok := conns.Load("daemon")
	if !ok || still != persistent {
		t.Fatal("persistent daemon conn was replaced")
	}
	if _, ok := conns.Load(assigned); !ok {
		t.Fatal("conns missing daemon-temp connection")
	}
	registeredMutex.RLock()
	got := registered["daemon"]
	_, tempOK := registered[assigned]
	registeredMutex.RUnlock()
	if got != persistentReg {
		t.Fatal("persistent daemon registration was replaced")
	}
	if !tempOK {
		t.Fatalf("registered[%q] missing", assigned)
	}

	_ = daemonClient.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	var buf [1]byte
	_, err = daemonClient.Read(buf[:])
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("persistent daemon connection closed: %v", err)
	}
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("persistent daemon read: %v", err)
	}

	_ = tempClient.Close()
	waitHandler(t, tempDone)
	_ = daemonClient.Close()
	waitHandler(t, daemonDone)
}

// TestReregisterStormUnixSocket re-registers one id over a real UNIX socket.
// (*net.UnixConn).File().Fd() clears O_NONBLOCK on the shared open file
// description while poll.FD.isBlocking stays 0. The old handler then blocks
// in Read, and oldConn.Close waits forever, so the new handler never sends
// its register response. The client deadline bounds that failure. The client
// fd itself stays non-blocking, so a later read on A cannot hang the package.
func TestReregisterStormUnixSocket(t *testing.T) {
	snapshotHubRegistry(t)
	t.Setenv("AEGIS_DEV_MODE", "1")
	t.Setenv("SUDO_USER", "")

	sock := filepath.Join(t.TempDir(), "hub.sock")
	ln, err := listenHubUnixSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	fi, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("hub socket mode = %o, want 0600", fi.Mode().Perm())
	}

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const id = "storm-client"
	conns := &sync.Map{}

	const iters = 200
	for i := 0; i < iters; i++ {
		clientA, _, decA, doneA := stormRegister(t, i, ln, sock, conns, id, pub)
		clientB, serverB, decB, doneB := stormRegister(t, i, ln, sock, conns, id, pub)

		got, ok := conns.Load(id)
		if !ok || got != serverB {
			t.Fatalf("iter %d: conns[%s] is not B", i, id)
		}
		registeredMutex.RLock()
		reg := registered[id]
		registeredMutex.RUnlock()
		if reg == nil {
			t.Fatalf("iter %d: %s dropped from the registry", i, id)
		}

		_ = clientB.SetDeadline(time.Now().Add(3 * time.Second))
		probe := Message{
			Source:      id,
			Destination: "hub",
			Command:     "get-version",
			Payload:     map[string]string{"from": "B"},
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
			Signature:   "dummy",
		}
		if err := json.NewEncoder(clientB).Encode(probe); err != nil {
			t.Fatalf("iter %d: B encode: %v", i, err)
		}
		var reply map[string]interface{}
		if err := decB.Decode(&reply); err != nil {
			t.Fatalf("iter %d: B decode: %v", i, err)
		}
		if errVal, bad := reply["error"]; bad {
			t.Fatalf("iter %d: B exchange error: %v", i, errVal)
		}
		if reply["status"] != "ok" {
			t.Fatalf("iter %d: B exchange reply: %#v", i, reply)
		}
		_ = clientB.SetDeadline(time.Time{})

		readErr := make(chan error, 1)
		go func() {
			_ = clientA.SetReadDeadline(time.Now().Add(time.Second))
			var discard json.RawMessage
			readErr <- decA.Decode(&discard)
		}()
		select {
		case err := <-readErr:
			if !stormPeerClosed(err) {
				t.Fatalf("iter %d: conn A still open: %v", i, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("iter %d: conn A read did not return within 2s", i)
		}
		select {
		case <-doneA:
		case <-time.After(2 * time.Second):
			t.Fatalf("iter %d: replaced handler did not exit", i)
		}

		_ = clientB.Close()
		select {
		case <-doneB:
		case <-time.After(2 * time.Second):
			t.Fatalf("iter %d: B handler did not exit", i)
		}
	}
}

func stormRegister(t *testing.T, iter int, ln net.Listener, sock string, conns *sync.Map, id string, pub ed25519.PublicKey) (client net.Conn, server net.Conn, dec *json.Decoder, done <-chan struct{}) {
	t.Helper()
	accErr := make(chan error, 1)
	accC := make(chan net.Conn, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			accErr <- aerr
			return
		}
		accC <- c
	}()
	client, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("iter %d: dial: %v", iter, err)
	}
	t.Cleanup(func() { _ = client.Close() })

	select {
	case err := <-accErr:
		t.Fatalf("iter %d: accept: %v", iter, err)
	case server = <-accC:
	case <-time.After(3 * time.Second):
		t.Fatalf("iter %d: accept timed out", iter)
	}

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		handleConnection(server, conns)
	}()

	reg := Message{
		Source:      id,
		Destination: "hub",
		Command:     "register",
		Payload: map[string]string{
			"public_key": base64.StdEncoding.EncodeToString(pub),
			"version":    "test",
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Signature: "dummy",
	}
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(client).Encode(reg); err != nil {
		t.Fatalf("iter %d: register encode: %v", iter, err)
	}
	dec = json.NewDecoder(client)
	var resp map[string]interface{}
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("iter %d: register decode: %v", iter, err)
	}
	if errVal, bad := resp["error"]; bad {
		t.Fatalf("iter %d: register error: %v", iter, errVal)
	}
	if resp["status"] != "registered" || resp["assigned_id"] != id {
		t.Fatalf("iter %d: register response: %#v", iter, resp)
	}
	_ = client.SetDeadline(time.Time{})
	return client, server, dec, doneCh
}

func stormPeerClosed(err error) bool {
	if err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		return false
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return false
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed)
}
