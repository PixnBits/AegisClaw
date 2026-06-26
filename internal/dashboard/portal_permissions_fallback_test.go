package dashboard

import (
	"context"
	"encoding/json"
	"testing"
)

type panelFallbackClient struct {
	calls []string
}

func (c *panelFallbackClient) Call(_ context.Context, action string, _ json.RawMessage) (*APIResponse, error) {
	c.calls = append(c.calls, action)
	switch action {
	case "permission.panel":
		return &APIResponse{Success: false, Error: "ERR_ACL_VIOLATION"}, nil
	case "permission.list":
		return &APIResponse{Success: true, Data: json.RawMessage(`[{"capability":"channel.post"}]`)}, nil
	case "permission.requests.list", "visibility.list":
		return &APIResponse{Success: true, Data: json.RawMessage(`[]`)}, nil
	case "permission.snapshot":
		return &APIResponse{Success: true, Data: json.RawMessage(`{"subject":"court-persona-architect"}`)}, nil
	default:
		return &APIResponse{Success: true, Data: json.RawMessage(`{}`)}, nil
	}
}

func TestFetchPermissionPanelFallsBackWhenPanelDenied(t *testing.T) {
	c := &panelFallbackClient{}
	srv, _ := New("127.0.0.1:0", c)
	out, err := srv.fetchPermissionPanel(context.Background(), "court-persona-architect")
	if err != nil {
		t.Fatal(err)
	}
	if out["agent_id"] != "court-persona-architect" {
		t.Fatalf("agent_id=%v", out["agent_id"])
	}
	grants, ok := out["grants"].([]interface{})
	if !ok || len(grants) == 0 {
		t.Fatalf("expected fallback grants, got %v", out["grants"])
	}
	if len(c.calls) < 2 || c.calls[0] != "permission.panel" || c.calls[1] != "permission.list" {
		t.Fatalf("calls=%v", c.calls)
	}
}