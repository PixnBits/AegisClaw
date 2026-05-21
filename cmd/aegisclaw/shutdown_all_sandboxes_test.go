package main

import (
	"context"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/sandbox"
)

// recordingSandboxRuntime implements sandboxLifecycleRuntime for DB-01 tests.
type recordingSandboxRuntime struct {
	infos   []sandbox.SandboxInfo
	stops   []string
	deletes []string
}

func (r *recordingSandboxRuntime) List(context.Context) ([]sandbox.SandboxInfo, error) {
	out := make([]sandbox.SandboxInfo, len(r.infos))
	copy(out, r.infos)
	return out, nil
}

func (r *recordingSandboxRuntime) Stop(_ context.Context, id string) error {
	r.stops = append(r.stops, id)
	return nil
}

func (r *recordingSandboxRuntime) Delete(_ context.Context, id string) error {
	r.deletes = append(r.deletes, id)
	return nil
}

func TestShutdownAllSandboxes_StopRunningThenDeleteAll(t *testing.T) {
	ctx := context.Background()
	rt := &recordingSandboxRuntime{
		infos: []sandbox.SandboxInfo{
			{Spec: sandbox.SandboxSpec{ID: "vm-a"}, State: sandbox.StateRunning},
			{Spec: sandbox.SandboxSpec{ID: "vm-b"}, State: sandbox.StateStopped},
		},
	}
	if err := shutdownAllSandboxes(ctx, rt); err != nil {
		t.Fatalf("shutdownAllSandboxes: %v", err)
	}
	wantStops := []string{"vm-a"}
	if len(rt.stops) != len(wantStops) {
		t.Fatalf("stops: got %#v want %#v", rt.stops, wantStops)
	}
	wantDeletes := []string{"vm-a", "vm-b"}
	if len(rt.deletes) != len(wantDeletes) {
		t.Fatalf("deletes: got %#v want %#v", rt.deletes, wantDeletes)
	}
}

func TestShutdownAllSandboxes_NilRuntime(t *testing.T) {
	if err := shutdownAllSandboxes(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}
