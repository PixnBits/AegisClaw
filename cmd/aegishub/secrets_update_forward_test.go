package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestIsStoreSecretsUpdate(t *testing.T) {
	ok := Message{Source: "store", Destination: "network-boundary", Command: "secrets.update"}
	if !isStoreSecretsUpdate("store", ok) {
		t.Fatal("store secrets.update to network-boundary = false")
	}
	cases := []struct {
		name string
		id   string
		msg  Message
	}{
		{"wrong componentID", "network-boundary", ok},
		{"wrong source", "store", Message{Source: "builder", Destination: "network-boundary", Command: "secrets.update"}},
		{"wrong dest", "store", Message{Source: "store", Destination: "store", Command: "secrets.update"}},
		{"wrong command", "store", Message{Source: "store", Destination: "network-boundary", Command: "secrets.push"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if isStoreSecretsUpdate(tc.id, tc.msg) {
				t.Fatal("got true, want false")
			}
		})
	}
}

func TestStoreSecretsUpdateForward(t *testing.T) {
	t.Setenv("AEGIS_DEV_MODE", "1")

	origRules := aclRules
	origPath := aclFilePath
	origMod := lastACLModTime
	t.Cleanup(func() {
		aclRules = origRules
		aclFilePath = origPath
		lastACLModTime = origMod
		registeredMutex.Lock()
		delete(registered, "store")
		delete(registered, "network-boundary")
		registeredMutex.Unlock()
	})

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Setenv("AEGIS_ACL_FILE", filepath.Join(wd, "..", "..", "config", "acls.yaml"))
	loadACL()
	if len(aclRules) == 0 {
		t.Fatal("aclRules empty after loadACL")
	}
	if !checkACL("store", "network-boundary", "secrets.update") || !checkACL("store", "network-boundary", "secrets.push") {
		t.Fatal("repo ACL does not grant store -> network-boundary secrets.update and secrets.push")
	}

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	conns := &sync.Map{}
	storeConn, _, storeDone := registerTestComponent(t, conns, "store", pub)
	t.Cleanup(func() {
		_ = storeConn.Close()
		waitHandler(t, storeDone)
	})
	nbConn, nbDec, nbDone := registerTestComponent(t, conns, "network-boundary", pub)
	t.Cleanup(func() {
		_ = nbConn.Close()
		waitHandler(t, nbDone)
	})

	payload := map[string]interface{}{"ciphertext": "blob-1", "nonce": "n-1"}
	ts := time.Now().UTC().Format(time.RFC3339)
	writeHubMessage(t, storeConn, Message{
		Source:      "store",
		Destination: "network-boundary",
		Command:     "secrets.update",
		Payload:     payload,
		Timestamp:   ts,
		Signature:   "dummy",
	})
	got := readHubMessage(t, nbConn, nbDec)
	if got.Command != "secrets.update" || got.Source != "store" || got.Destination != "network-boundary" || got.Signature != "dummy" || got.Timestamp != ts {
		t.Fatalf("forwarded %#v", got)
	}
	if !jsonEqual(got.Payload, payload) {
		t.Fatalf("payload %#v, want %#v", got.Payload, payload)
	}
	assertNoFrame(t, storeConn)

	writeHubMessage(t, storeConn, Message{
		Source:      "store",
		Destination: "network-boundary",
		Command:     "secrets.push",
		Payload:     payload,
		Timestamp:   ts,
		Signature:   "dummy",
	})
	assertNoFrame(t, nbConn)

}

// The forward re-checks the ACL: without a store -> network-boundary
// secrets.update grant the message is refused and not delivered. Rules are
// set before the connections start, since aclRules is read without a lock.
func TestStoreSecretsUpdateForwardACLDenied(t *testing.T) {
	t.Setenv("AEGIS_DEV_MODE", "1")

	origRules := aclRules
	t.Cleanup(func() {
		aclRules = origRules
		registeredMutex.Lock()
		delete(registered, "store")
		delete(registered, "network-boundary")
		registeredMutex.Unlock()
	})
	aclRules = []ACLRule{{
		Source:      "store",
		Destination: "network-boundary",
		Commands:    []string{"secrets.push"},
	}}

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	conns := &sync.Map{}
	storeConn, storeDec, storeDone := registerTestComponent(t, conns, "store", pub)
	t.Cleanup(func() {
		_ = storeConn.Close()
		waitHandler(t, storeDone)
	})
	nbConn, _, nbDone := registerTestComponent(t, conns, "network-boundary", pub)
	t.Cleanup(func() {
		_ = nbConn.Close()
		waitHandler(t, nbDone)
	})

	payload := map[string]interface{}{"ciphertext": "blob-1", "nonce": "n-1"}
	ts := time.Now().UTC().Format(time.RFC3339)
	writeHubMessage(t, storeConn, Message{
		Source:      "store",
		Destination: "network-boundary",
		Command:     "secrets.update",
		Payload:     payload,
		Timestamp:   ts,
		Signature:   "dummy",
	})
	if err := storeConn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var denied map[string]interface{}
	if err := storeDec.Decode(&denied); err != nil {
		t.Fatalf("acl violation decode: %v", err)
	}
	if err := storeConn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if denied["error"] != "ERR_ACL_VIOLATION" {
		t.Fatalf("store response %#v, want ERR_ACL_VIOLATION", denied)
	}
	assertNoFrame(t, nbConn)
}

// forwardStoreSecretsUpdate is its own gate: called directly, past the read
// loop's general ACL check, it refuses a secrets.update the ACL does not grant
// and network-boundary receives nothing. The ACL is set before the hub starts.
func TestForwardStoreSecretsUpdateChecksACL(t *testing.T) {
	t.Setenv("AEGIS_DEV_MODE", "1")

	origRules := aclRules
	t.Cleanup(func() {
		aclRules = origRules
		registeredMutex.Lock()
		delete(registered, "network-boundary")
		registeredMutex.Unlock()
	})
	aclRules = []ACLRule{{
		Source:      "store",
		Destination: "network-boundary",
		Commands:    []string{"secrets.push"},
	}}

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nbConn, _, nbDone := registerTestComponent(t, &sync.Map{}, "network-boundary", pub)
	t.Cleanup(func() {
		_ = nbConn.Close()
		waitHandler(t, nbDone)
	})

	msg := Message{
		Source:      "store",
		Destination: "network-boundary",
		Command:     "secrets.update",
		Payload:     map[string]interface{}{"ciphertext": "blob-1", "nonce": "n-1"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Signature:   "dummy",
	}
	// net.Pipe writes block until read, so forward in a goroutine and read
	// network-boundary's side concurrently.
	forwarded := make(chan bool, 1)
	go func() { forwarded <- forwardStoreSecretsUpdate(msg) }()
	assertNoFrame(t, nbConn)
	select {
	case ok := <-forwarded:
		if ok {
			t.Fatal("forwardStoreSecretsUpdate = true without an ACL grant")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("forwardStoreSecretsUpdate blocked writing to network-boundary without an ACL grant")
	}
}

func writeHubMessage(t *testing.T, conn net.Conn, msg Message) {
	t.Helper()
	if err := conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(conn).Encode(msg); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
}

func readHubMessage(t *testing.T, conn net.Conn, dec *json.Decoder) Message {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var msg Message
	if err := dec.Decode(&msg); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	return msg
}

func assertNoFrame(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var buf [1]byte
	_, err := conn.Read(buf[:])
	if clearErr := conn.SetReadDeadline(time.Time{}); clearErr != nil {
		t.Fatal(clearErr)
	}
	if err == nil {
		t.Fatalf("unexpected frame starting with %q", buf)
	}
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read: %v", err)
	}
}

func jsonEqual(a, b interface{}) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ab) == string(bb)
}
