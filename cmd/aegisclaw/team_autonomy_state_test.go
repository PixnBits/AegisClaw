package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTeamRegistryCreateJoinLeave(t *testing.T) {
	dir := t.TempDir()
	reg, err := newTeamRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	team, err := reg.create("research")
	if err != nil {
		t.Fatal(err)
	}
	if team.ID == "" {
		t.Fatal("expected team id")
	}
	if err := reg.join(team.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := reg.join(team.ID, "alice"); err != nil {
		t.Fatal("idempotent join")
	}
	rec, ok := reg.get(team.ID)
	if !ok || len(rec.Members) != 1 {
		t.Fatalf("members = %v", rec.Members)
	}
	if err := reg.leave(team.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	rec, ok = reg.get(team.ID)
	if !ok || len(rec.Members) != 0 {
		t.Fatalf("expected empty members after leave, got %v", rec.Members)
	}

	// Reload from disk.
	reg2, err := newTeamRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec, ok = reg2.get(team.ID)
	if !ok || rec.Name != "research" {
		t.Fatalf("persisted team: %+v ok=%v", rec, ok)
	}
}

func TestAutonomyGrantRevokeReset(t *testing.T) {
	dir := t.TempDir()
	reg, err := newAutonomyRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	sid := "session-abc"
	until := time.Now().Add(time.Hour)
	if err := reg.grant(sid, "researcher", "tools", until); err != nil {
		t.Fatal(err)
	}
	rec, ok := reg.show(sid)
	if !ok || rec.Preset != "researcher" {
		t.Fatalf("grant: %+v ok=%v", rec, ok)
	}
	if err := reg.revoke(sid, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.show(sid); ok {
		t.Fatal("expected revoked")
	}
	if err := reg.grant(sid, "default", "", time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := reg.reset(sid); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.show(sid); ok {
		t.Fatal("expected reset")
	}

	path := filepath.Join(dir, "autonomy.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("autonomy file: %v", err)
	}
	reg2, err := newAutonomyRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg2.Items) != 0 {
		t.Fatalf("expected empty autonomy after reset persist, got %d", len(reg2.Items))
	}
}

func TestAutonomyResetRequiresSessionID(t *testing.T) {
	reg, err := newAutonomyRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.reset("   "); err == nil {
		t.Fatal("expected session_id validation error")
	}
}

func TestAutonomyShowExpiresPastGrant(t *testing.T) {
	reg, err := newAutonomyRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sid := "session-expired"
	if err := reg.grant(sid, "default", "", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.show(sid); ok {
		t.Fatal("expected expired autonomy grant to be removed")
	}
}
