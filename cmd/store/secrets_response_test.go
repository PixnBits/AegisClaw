package main

import "testing"

func TestDispatchSecretsResponse(t *testing.T) {
	chdirTempAssertNoPackageAudit(t)
	w := testStoreWorld(t)

	withErr := Message{}
	skip := dispatchStoreCommand(Message{
		Source:      "network-boundary",
		Destination: "store",
		Command:     "secrets.response",
		Payload:     map[string]interface{}{"error": "bad signature", "skills": float64(2)},
	}, &withErr, w)
	if !skip {
		t.Fatal("secrets.response with error: skip=false, want true")
	}
	if withErr.Command != "" {
		t.Fatalf("secrets.response with error: command %q, want empty", withErr.Command)
	}

	noErr := Message{}
	skip = dispatchStoreCommand(Message{
		Source:      "network-boundary",
		Destination: "store",
		Command:     "secrets.response",
		Payload:     map[string]interface{}{"status": "ok", "skills": float64(2)},
	}, &noErr, w)
	if !skip {
		t.Fatal("secrets.response without error: skip=false, want true")
	}
	if noErr.Command != "" {
		t.Fatalf("secrets.response without error: command %q, want empty", noErr.Command)
	}

	other := Message{}
	skip = dispatchStoreCommand(Message{
		Source:      "builder",
		Destination: "store",
		Command:     "secrets.response",
		Payload:     map[string]interface{}{"error": "nope"},
	}, &other, w)
	if skip {
		t.Fatal("secrets.response from builder: skip=true, want false")
	}
	if other.Command != "error" {
		t.Fatalf("secrets.response from builder: command %q, want error", other.Command)
	}
	if other.Payload != "unknown command" {
		t.Fatalf("payload %#v, want unknown command", other.Payload)
	}
}
