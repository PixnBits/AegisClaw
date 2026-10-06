package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"

	"AegisClaw/internal/sandbox"
)

// countingBackend records sandbox calls. EnsureRoleAgent must not reach
// Status or Start for a reserved id.
type countingBackend struct {
	starts   int
	statuses int
}

func (b *countingBackend) Start(context.Context, sandbox.VMConfig) error { b.starts++; return nil }
func (b *countingBackend) Stop(context.Context, string) error            { return nil }
func (b *countingBackend) Status(context.Context, string) (sandbox.Status, error) {
	b.statuses++
	return sandbox.StatusStopped, nil
}
func (b *countingBackend) List(context.Context) ([]sandbox.VMInfo, error) { return nil, nil }
func (b *countingBackend) Cleanup(context.Context) error                  { return nil }
func (b *countingBackend) BootPhases(context.Context, string) map[string]int64 {
	return nil
}

func TestEnsureRoleAgentRejectsReservedBeforeStart(t *testing.T) {
	cases := []struct{ role, channel string }{
		{"court-persona-ciso", ""},
		{"court", "persona-ciso"},
		{"court", "ciso"},
		{"store", ""},
		{"network-boundary", ""},
		{"web-portal", ""},
		{"court-scribe", ""},
		{"daemon", ""},
		{"daemon", "orchestrator"},
		{"memory", "abc"},
		{"builder", ""},
		{"hub", ""},
		{"Agent", "sess"},
	}
	for _, tc := range cases {
		name := tc.role
		if tc.channel != "" {
			name += "/" + tc.channel
		}
		t.Run(name, func(t *testing.T) {
			backend := &countingBackend{}
			var starts []string
			o := &Orchestrator{
				backend: backend,
				vms:     map[string]*VMLifecycle{},
				startVM: func(ctx context.Context, vmType, id, image string) error {
					starts = append(starts, vmType+" "+id+" "+image)
					return nil
				},
			}
			_, err := o.EnsureRoleAgent(context.Background(), tc.role, tc.channel)
			if !errors.Is(err, ErrReservedRoleID) {
				t.Fatalf("err=%v", err)
			}
			if len(starts) != 0 || backend.starts != 0 || backend.statuses != 0 {
				t.Fatalf("StartVM calls=%v backend.Start=%d GetVMStatus=%d", starts, backend.starts, backend.statuses)
			}
		})
	}
}

func TestEnsureRoleAgentNormalRoleUsesAgentImageFallback(t *testing.T) {
	backend := &countingBackend{}
	var calls []string
	o := &Orchestrator{
		backend: backend,
		vms:     map[string]*VMLifecycle{},
		startVM: func(ctx context.Context, vmType, id, image string) error {
			calls = append(calls, vmType+" "+id+" "+image)
			if image != "agent.img" {
				return errors.New("missing image")
			}
			return nil
		},
	}
	id, err := o.EnsureRoleAgent(context.Background(), "coder", "plan-1")
	if err != nil {
		t.Fatal(err)
	}
	if id != "coder-plan-1" {
		t.Fatalf("id=%q", id)
	}
	if len(calls) != 2 || calls[0] != "coder coder-plan-1 coder.img" || calls[1] != "agent coder-plan-1 agent.img" {
		t.Fatalf("StartVM calls=%v", calls)
	}
	if backend.starts != 0 {
		t.Fatalf("sandbox Start reached %d times; hook should replace StartVM", backend.starts)
	}
	if backend.statuses != 1 {
		t.Fatalf("GetVMStatus calls=%d want 1", backend.statuses)
	}
}

func TestEnsureRoleAgentProjectManagerNotReserved(t *testing.T) {
	var calls []string
	o := &Orchestrator{
		backend: &countingBackend{},
		vms:     map[string]*VMLifecycle{},
		startVM: func(ctx context.Context, vmType, id, image string) error {
			calls = append(calls, vmType+" "+id+" "+image)
			return nil
		},
	}
	id, err := o.EnsureRoleAgent(context.Background(), "project-manager", "main")
	if err != nil {
		t.Fatal(err)
	}
	if id != "project-manager-main" {
		t.Fatalf("id=%q", id)
	}
	if len(calls) != 1 || calls[0] != "project-manager project-manager-main project-manager.img" {
		t.Fatalf("StartVM calls=%v", calls)
	}
}

func TestEnsureRoleAgentPairedPathNotReserved(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	o := &Orchestrator{
		vms: map[string]*VMLifecycle{},
		startVM: func(ctx context.Context, vmType, id, image string) error {
			mu.Lock()
			calls = append(calls, vmType+" "+id+" "+image)
			mu.Unlock()
			return nil
		},
	}
	id, err := o.EnsureRoleAgent(context.Background(), "agent", "sess")
	if err != nil {
		t.Fatal(err)
	}
	if id != "agent-sess" {
		t.Fatalf("id=%q", id)
	}
	if !hasCall(calls, "memory memory-sess memory.img") || !hasCall(calls, "agent agent-sess agent.img") {
		t.Fatalf("paired starts=%v", calls)
	}

	calls = nil
	id, err = o.EnsureRoleAgent(context.Background(), "", "sess")
	if err != nil {
		t.Fatal(err)
	}
	if id != "agent-sess" {
		t.Fatalf("empty role id=%q", id)
	}
	if !hasCall(calls, "memory memory-sess memory.img") || !hasCall(calls, "agent agent-sess agent.img") {
		t.Fatalf("empty-role paired starts=%v", calls)
	}
}

func TestEnsureRoleAgentRejectsUnsafeBeforeStart(t *testing.T) {
	cases := []struct{ role, channel string }{
		{"../x", ""},
		{"coder", "../../../x"},
		{"Coder", ""},
		{"Coder", "1"},
		{"coder-1", ""},
		{"coder", "Plan-1"},
		{"general", "main"},
		{"sdlc-coder", "main"},
		{"senior-coder", "1"},
		{"agent", "../../../x"},
		{"", "../../../x"},
		{"store", ""},
		{"court", "persona-ciso"},
		{"court-persona-ciso", ""},
		{"network-boundary", ""},
		{"web-portal", ""},
		{"court-scribe", ""},
	}
	for _, tc := range cases {
		name := tc.role + "/" + tc.channel
		t.Run(name, func(t *testing.T) {
			var starts int
			o := &Orchestrator{
				vms: map[string]*VMLifecycle{},
				startVM: func(context.Context, string, string, string) error {
					starts++
					return nil
				},
			}
			_, err := o.EnsureRoleAgent(context.Background(), tc.role, tc.channel)
			if !EnsureRoleRefused(err) {
				t.Fatalf("err=%v", err)
			}
			if starts != 0 {
				t.Fatalf("StartVM calls=%d", starts)
			}
		})
	}
}

func TestStartVMValidatesBeforeHook(t *testing.T) {
	calls := 0
	o := &Orchestrator{
		startVM: func(context.Context, string, string, string) error {
			calls++
			return nil
		},
	}
	for _, id := range []string{"../../../x", "../x", "Coder-1", "coder-", "foo--bar", ""} {
		calls = 0
		err := o.StartVM(context.Background(), "agent", id, "agent.img")
		if !errors.Is(err, ErrInvalidVMID) || calls != 0 {
			t.Errorf("StartVM(%q) err=%v calls=%d", id, err, calls)
		}
	}
	// Reserved infrastructure ids are real StartVM targets. The reserved
	// list is an ensure_role rule, not a StartVM rule.
	for _, id := range []string{
		"store", "network-boundary", "web-portal", "court-scribe", "aegishub",
		"court-persona-ciso", "court-persona-security-architect",
		"builder-1", "builder-sit-1", "agent-abc", "memory-abc",
		"project-manager-main",
	} {
		calls = 0
		if err := o.StartVM(context.Background(), "x", id, "x.img"); err != nil || calls != 1 {
			t.Errorf("StartVM(%q) err=%v calls=%d", id, err, calls)
		}
	}
}

func hasCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}
