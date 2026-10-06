package main

import (
	"encoding/json"
	"net"
	"testing"
	"time"
)

// network-boundary -> store grants audit.append exactly (#156). Nothing else
// under audit, no wildcard that covers it, no neighbouring source or
// destination, and no audit.appended back: the append is a one-way push.
func TestRepoACLBoundaryAuditAppendExact(t *testing.T) {
	loadRepoACL(t)

	if !checkACL("network-boundary", "store", "audit.append") {
		t.Fatal("network-boundary -> store audit.append = false, want allow")
	}
	for _, cmd := range []string{
		"audit.list", "audit.get_root", "audit.appended", "audit.appendx",
		"audit.append.x", "audit", "audit.", "audit.*", "audit.delete",
	} {
		if checkACL("network-boundary", "store", cmd) {
			t.Errorf("network-boundary -> store %s = true, want deny", cmd)
		}
	}
	for _, cmd := range []string{"audit.appended", "audit.append"} {
		if checkACL("store", "network-boundary", cmd) {
			t.Errorf("store -> network-boundary %s = true, want deny", cmd)
		}
	}
	for _, src := range []string{
		"network-boundary-1", "network-boundaryx", "network", "network-",
		"web-portal", "coder-1", "agent-1", "project-manager", "court-persona-ciso",
		"memory", "tester-1", "aegis-cli-internal", "channel-facilitator",
	} {
		if checkACL(src, "store", "audit.append") {
			t.Errorf("%s -> store audit.append = true, want deny", src)
		}
	}
	for _, dst := range []string{"store-1", "storex", "memory", "builder", "web-portal", "court-persona-ciso"} {
		if checkACL("network-boundary", dst, "audit.append") {
			t.Errorf("network-boundary -> %s audit.append = true, want deny", dst)
		}
	}
	// Every rule that applies to network-boundary -> store grants audit.append
	// only by the exact name, and none of them grants any other audit command.
	for _, rule := range aclRules {
		if !aclIDMatch(rule.Source, "network-boundary") || !aclIDMatch(rule.Destination, "store") {
			continue
		}
		for _, c := range rule.Commands {
			if aclMatch(c, "audit.append") && c != "audit.append" {
				t.Errorf("rule %q -> %q grants audit.append through %q, want the exact command", rule.Source, rule.Destination, c)
			}
			if aclMatch(c, "audit.list") || aclMatch(c, "audit.x") {
				t.Errorf("rule %q -> %q pattern %q covers other audit commands", rule.Source, rule.Destination, c)
			}
		}
	}
}

// audit.append is a one-way push: the hub returns to the sender without
// waiting for Store, which does not reply and has no audit.appended grant.
func TestForwardHubRPC_AuditAppendDoesNotWaitForStoreReply(t *testing.T) {
	destClient, destHub := net.Pipe()
	defer destClient.Close()
	defer destHub.Close()
	registeredMutex.Lock()
	registered["store-audit-test"] = &RegisteredComponent{ID: "store-audit-test", Encoders: &ComponentEncoders{
		Encoder: json.NewEncoder(destHub),
		Decoder: json.NewDecoder(destHub),
	}}
	registeredMutex.Unlock()
	defer func() {
		registeredMutex.Lock()
		delete(registered, "store-audit-test")
		registeredMutex.Unlock()
	}()

	got := make(chan Message, 1)
	go func() {
		var m Message
		if json.NewDecoder(destClient).Decode(&m) == nil {
			got <- m
		}
	}()

	start := time.Now()
	reply := forwardHubRPC("network-boundary", Message{
		Source:      "network-boundary",
		Destination: "store-audit-test",
		Command:     "audit.append",
		Payload:     map[string]interface{}{"action": "blocked_request"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	})
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("audit.append waited %s; a push must not wait for a Store reply", elapsed)
	}
	if reply.Command != "response" {
		t.Fatalf("audit.append push reply %q (%v), want response", reply.Command, reply.Payload)
	}
	select {
	case m := <-got:
		if m.Command != "audit.append" || m.Source != "network-boundary" {
			t.Fatalf("store got %q from %q", m.Command, m.Source)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("audit.append was not delivered to the destination")
	}
}
