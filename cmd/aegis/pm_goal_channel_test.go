package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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

type pmGoalExitCode int

// runPMGoalChannelStep runs runPMGoal with the seams replaced. A call to
// pmGoalExit panics with the code so the rest of runPMGoal (which needs a
// live hub) never runs; a run that does not exit returns -1 only if the
// channel step passed, which these cases never expect.
func runPMGoalChannelStep(t *testing.T, chID string, send pmGoalHubSend) (code int, stderr string, sends int) {
	t.Helper()
	var buf strings.Builder
	oldExit, oldErr, oldSend := pmGoalExit, pmGoalStderr, pmGoalHubSendFn
	t.Cleanup(func() { pmGoalExit, pmGoalStderr, pmGoalHubSendFn = oldExit, oldErr, oldSend })
	pmGoalStderr = &buf
	pmGoalExit = func(c int) { panic(pmGoalExitCode(c)) }
	pmGoalHubSendFn = func(target, cmd string, payload interface{}) (pmGoalHubReply, error) {
		sends++
		return send(target, cmd, payload)
	}
	cmd := &cobra.Command{Use: "goal"}
	cmd.Flags().String("channel", "", "")
	if err := cmd.Flags().Set("channel", chID); err != nil {
		t.Fatal(err)
	}
	code = -1
	func() {
		defer func() {
			if r := recover(); r != nil {
				c, ok := r.(pmGoalExitCode)
				if !ok {
					panic(r)
				}
				code = int(c)
			}
		}()
		runPMGoal(cmd, []string{"ship", "it"})
	}()
	return code, buf.String(), sends
}

func TestRunPMGoalExitsNonZeroOnBadChannel(t *testing.T) {
	t.Run("invalid id", func(t *testing.T) {
		code, stderr, sends := runPMGoalChannelStep(t, "MyProj", func(string, string, interface{}) (pmGoalHubReply, error) {
			t.Fatal("hub must not be called for an invalid id")
			return pmGoalHubReply{}, nil
		})
		if code != 1 || sends != 0 {
			t.Fatalf("code=%d sends=%d", code, sends)
		}
		if !strings.Contains(stderr, "pm goal: invalid channel id") {
			t.Fatalf("stderr %q", stderr)
		}
	})
	t.Run("create refused", func(t *testing.T) {
		code, stderr, sends := runPMGoalChannelStep(t, "new-proj", func(_, cmd string, _ interface{}) (pmGoalHubReply, error) {
			if cmd == "channel.get" {
				return pmGoalHubReply{}, fmt.Errorf("not found")
			}
			return pmGoalHubReply{Command: "error", Payload: "invalid channel id: store refused"}, nil
		})
		if code != 1 || sends != 2 {
			t.Fatalf("code=%d sends=%d", code, sends)
		}
		if !strings.Contains(stderr, "pm goal: channel.create: invalid channel id: store refused") {
			t.Fatalf("stderr %q", stderr)
		}
	})
}
