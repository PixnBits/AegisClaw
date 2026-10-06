package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestEnsurePMGoalChannel(t *testing.T) {
	t.Run("invalid id", func(t *testing.T) {
		calls := 0
		err := ensurePMGoalChannel("MyProj", func(target, cmd string, payload interface{}) (pmGoalHubReply, error) {
			calls++
			return pmGoalHubReply{Payload: map[string]interface{}{"id": "MyProj"}}, nil
		})
		if err == nil || !strings.Contains(err.Error(), "invalid channel id") {
			t.Fatalf("err=%v", err)
		}
		if calls != 0 {
			t.Fatalf("hub calls=%d", calls)
		}
	})

	t.Run("existing channel", func(t *testing.T) {
		var cmds []string
		err := ensurePMGoalChannel("main", func(target, cmd string, payload interface{}) (pmGoalHubReply, error) {
			cmds = append(cmds, cmd)
			if cmd == "channel.create" {
				t.Fatal("create called for an existing channel")
			}
			return pmGoalHubReply{Command: "channel.data", Payload: map[string]interface{}{"id": "main"}}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(cmds) != 1 || cmds[0] != "channel.get" {
			t.Fatalf("cmds=%v", cmds)
		}
	})

	t.Run("create refused", func(t *testing.T) {
		err := ensurePMGoalChannel("plan-demo", func(target, cmd string, payload interface{}) (pmGoalHubReply, error) {
			if cmd == "channel.get" {
				return pmGoalHubReply{}, fmt.Errorf("missing")
			}
			return pmGoalHubReply{}, fmt.Errorf("invalid channel id: refused")
		})
		if err == nil || !strings.Contains(err.Error(), "refused") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("create error reply", func(t *testing.T) {
		err := ensurePMGoalChannel("plan-demo", func(target, cmd string, payload interface{}) (pmGoalHubReply, error) {
			if cmd == "channel.get" {
				return pmGoalHubReply{}, nil
			}
			return pmGoalHubReply{Command: "error", Payload: "invalid channel id: store refused"}, nil
		})
		if err == nil || !strings.Contains(err.Error(), "store refused") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("create ok", func(t *testing.T) {
		var cmds []string
		err := ensurePMGoalChannel("plan-demo", func(target, cmd string, payload interface{}) (pmGoalHubReply, error) {
			cmds = append(cmds, cmd)
			if cmd == "channel.get" {
				return pmGoalHubReply{}, fmt.Errorf("not found")
			}
			return pmGoalHubReply{Command: "channel.created", Payload: map[string]interface{}{"id": "plan-demo"}}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(cmds) != 2 || cmds[0] != "channel.get" || cmds[1] != "channel.create" {
			t.Fatalf("cmds=%v", cmds)
		}
	})
}
