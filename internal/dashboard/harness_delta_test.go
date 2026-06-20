package dashboard

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"AegisClaw/internal/dashboard/contracts"
	"AegisClaw/internal/portalstomp"
)

type harnessMockClient struct {
	state contracts.HarnessState
}

func (m *harnessMockClient) Call(_ context.Context, action string, _ json.RawMessage) (*APIResponse, error) {
	if action != "harness.get" {
		return &APIResponse{Success: true}, nil
	}
	body, _ := json.Marshal(m.state)
	return &APIResponse{Success: true, Data: body}, nil
}

func TestPublishHarnessDeltasEmitsStructuredEvents(t *testing.T) {
	hub := portalstomp.NewHub()
	sess := portalstomp.NewSession(hub)
	sess.HandleFrame("SUBSCRIBE", map[string]string{
		"id":          "sub-harness",
		"destination": contracts.HarnessUpdatesTopic("plan_main"),
	}, "")

	s := &Server{
		stompHub: hub,
		apiClient: &harnessMockClient{
			state: contracts.HarnessState{
				Plan: &contracts.Plan{
					PlanID:    "plan_main",
					ChannelID: "main",
					Goal:      "Compare Zig vs Rust",
					Stages:    contracts.DefaultStages(),
				},
				Tasks: []contracts.NarrowTask{
					{
						TaskID:       "task_1",
						PlanID:       "plan_main",
						AgentPersona: "researcher",
						Scope:        "Research Zig",
						CurrentStage: "Execute",
						Progress:     10,
					},
				},
			},
		},
	}

	s.publishHarnessDeltas(context.Background(), "main")

	var types []string
	for i := 0; i < 4; i++ {
		select {
		case frame := <-sess.Outbound():
			if !strings.Contains(frame, "MESSAGE") {
				continue
			}
			idx := strings.Index(frame, "\n\n")
			if idx < 0 {
				continue
			}
			body := frame[idx+2:]
			body = strings.TrimSuffix(body, "\x00")
			var m map[string]interface{}
			if json.Unmarshal([]byte(body), &m) == nil {
				if typ, _ := m["type"].(string); typ != "" {
					types = append(types, typ)
				}
			}
		default:
			break
		}
	}

	has := func(want string) bool {
		for _, t := range types {
			if t == want {
				return true
			}
		}
		return false
	}
	if !has(contracts.TypeHarnessPlanCreated) {
		t.Fatalf("missing plan created, got %v", types)
	}
	if !has(contracts.TypeHarnessTaskAssigned) {
		t.Fatalf("missing task assigned, got %v", types)
	}
}