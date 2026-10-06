package channelfacilitator

import (
	"context"
	"testing"

	"AegisClaw/internal/transport/hubclient"
)

type captureHub struct {
	msgs []hubclient.Message
}

func (c *captureHub) Send(_ context.Context, msg hubclient.Message) (hubclient.Message, error) {
	c.msgs = append(c.msgs, msg)
	return hubclient.Message{Command: "response"}, nil
}

func (c *captureHub) Fire(context.Context, hubclient.Message) error { return nil }

func TestEnsureRoleSkipsCourtPersona(t *testing.T) {
	h := &captureHub{}
	f := &Facilitator{hub: h}
	if err := f.ensureRole(context.Background(), "court-persona-ciso", "main"); err != nil {
		t.Fatal(err)
	}
	if err := f.ensureRole(context.Background(), "court-scribe", "main"); err != nil {
		t.Fatal(err)
	}
	if err := f.ensureRole(context.Background(), "coder", "main"); err != nil {
		t.Fatal(err)
	}
	var ensured []string
	for _, msg := range h.msgs {
		if msg.Command != "ensure.role" {
			continue
		}
		role, _ := msg.Payload.(map[string]interface{})["role"].(string)
		ensured = append(ensured, role)
	}
	if len(ensured) != 1 || ensured[0] != "coder" {
		t.Fatalf("ensure.role roles = %v, want [coder]", ensured)
	}
}
