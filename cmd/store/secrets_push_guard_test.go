package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func secretsPushWorld(t *testing.T) (*storeWorld, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	chdirTempAssertNoPackageAudit(t)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_SECRETS_SYMMETRIC_KEY", base64.StdEncoding.EncodeToString(key))
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sent := &bytes.Buffer{}
	w := testStoreWorld(t)
	w.encoder = json.NewEncoder(sent)
	w.priv = priv
	logs := &bytes.Buffer{}
	orig := securityLogWriter
	securityLogWriter = func() io.Writer { return logs }
	t.Cleanup(func() { securityLogWriter = orig })
	return w, sent, logs
}

func secretsSecurityLines(t *testing.T, logs *bytes.Buffer) []map[string]interface{} {
	t.Helper()
	var out []map[string]interface{}
	for _, line := range strings.Split(strings.TrimRight(logs.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, securityEventPrefix) {
			t.Fatalf("log line without %q: %q", securityEventPrefix, line)
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, securityEventPrefix)), &ev); err != nil {
			t.Fatalf("security line is not one JSON object: %q (%v)", line, err)
		}
		out = append(out, ev)
	}
	return out
}

// secrets.push is host-only: daemon and daemon-internal / daemon-internal-N.
// Every other source gets ERR_PERMISSION_DENIED and one SECURITY line, and
// nothing is sent to network-boundary. The source is checked before the
// payload is read.
func TestSecretsPushRefusesNonHostSources(t *testing.T) {
	payloads := []interface{}{
		map[string]interface{}{"secrets": map[string]interface{}{"skill-a": "v"}},
		"not an object",
		map[string]interface{}{"secrets": "nope"},
	}
	for _, src := range []string{
		"network-boundary", "agent-1", "web-portal", "daemon-internalx",
		"daemon-internal-", "daemonx", "daemon-temp-1", "aegis-daemon-temp-1",
		"aegis-cli-internal", "aegis-cli-internal-3", "channel-facilitator",
		"coder-1", "court-persona-ciso", "store", "", "agent-1\nSECURITY {\"event\":\"forged\"}",
	} {
		for _, p := range payloads {
			w, sent, logs := secretsPushWorld(t)
			resp := Message{Timestamp: "2026-10-06T00:00:00Z"}
			if skip := dispatchStoreCommand(Message{Source: src, Destination: "store", Command: "secrets.push", Payload: p}, &resp, w); skip {
				t.Fatalf("%q: skipReply = true", src)
			}
			if resp.Command != "error" || !strings.HasPrefix(resp.Payload.(string), "ERR_PERMISSION_DENIED") {
				t.Fatalf("secrets.push from %q payload %#v -> %q %#v, want ERR_PERMISSION_DENIED", src, p, resp.Command, resp.Payload)
			}
			if sent.Len() != 0 {
				t.Fatalf("secrets.push from %q sent %q", src, sent.String())
			}
			evs := secretsSecurityLines(t, logs)
			if len(evs) != 1 || evs[0]["event"] != storeSecretsPushRefusedEvent || evs[0]["source"] != src || evs[0]["severity"] != "security" {
				t.Fatalf("secrets.push from %q logged %#v, want one %s line", src, evs, storeSecretsPushRefusedEvent)
			}
		}
	}
}

func TestSecretsPushAcceptsHostSources(t *testing.T) {
	for _, src := range []string{"daemon-internal-1", "daemon-internal", "daemon-internal-42", "daemon"} {
		w, sent, logs := secretsPushWorld(t)
		resp := Message{Timestamp: "2026-10-06T00:00:00Z"}
		dispatchStoreCommand(Message{
			Source: src, Destination: "store", Command: "secrets.push",
			Payload: map[string]interface{}{"secrets": map[string]interface{}{"skill-a": "v", "skill-b": "w"}},
		}, &resp, w)
		if resp.Command != "secrets.pushed" {
			t.Fatalf("secrets.push from %q -> %q %#v, want secrets.pushed", src, resp.Command, resp.Payload)
		}
		var out Message
		if err := json.NewDecoder(sent).Decode(&out); err != nil {
			t.Fatalf("secrets.push from %q sent nothing: %v", src, err)
		}
		if out.Command != "secrets.update" || out.Destination != "network-boundary" || out.Source != "store" {
			t.Fatalf("secrets.push from %q sent %q %s -> %s", src, out.Command, out.Source, out.Destination)
		}
		if logs.Len() != 0 {
			t.Fatalf("secrets.push from %q logged %q", src, logs.String())
		}
	}
}

func TestIsSecretsPushSource(t *testing.T) {
	for src, want := range map[string]bool{
		"daemon": true, "daemon-internal": true, "daemon-internal-1": true, "daemon-internal-abc": true,
		"daemon-internal-": false, "daemon-internalx": false, "daemonx": false, "daemon-1": false,
		"daemon-temp-1": false, "aegis-daemon-temp": false, "aegis-cli-internal": false,
		"aegis-cli-internal-1": false, "channel-facilitator": false, "network-boundary": false,
		"web-portal": false, "agent-1": false, "Daemon-Internal-1": false, "": false,
	} {
		if got := isSecretsPushSource(src); got != want {
			t.Errorf("isSecretsPushSource(%q) = %v, want %v", src, got, want)
		}
	}
}
