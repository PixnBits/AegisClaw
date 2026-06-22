package channelfacilitator

import "testing"

func TestDedupeMemberRoles(t *testing.T) {
	members := []map[string]interface{}{
		{"role": "project-manager"},
		{"role": "coder"},
		{"role": "project-manager"},
		{"role": "coder"},
	}
	got := dedupeMemberRoles(members)
	if len(got) != 2 || got[0] != "project-manager" || got[1] != "coder" {
		t.Fatalf("dedupeMemberRoles() = %v, want [project-manager coder]", got)
	}
}

func TestFacilitatorActorSkeleton(t *testing.T) {
	// Skeleton: Facilitator provides per-channel single actor for serialization (spec §7).
	f := &Facilitator{actors: map[string]*ChannelActor{}}
	a1 := f.actorFor("ch1")
	a2 := f.actorFor("ch1")
	if a1 != a2 {
		t.Fatal("actorFor must return same actor for same channel")
	}
	a3 := f.actorFor("ch2")
	if a3 == a1 {
		t.Fatal("different channels must have distinct actors")
	}
	// Token channel acts as mutex (capacity 1).
	select {
	case a1.mu <- struct{}{}:
		// acquired
	default:
		t.Fatal("expected to acquire actor token")
	}
	// Release
	<-a1.mu
}

func TestTurnDestinations(t *testing.T) {
	// Part of wiring: ensure correct hub destinations for turn delivery per role/channel.
	if got := turnDestinations("coder", "chX"); len(got) == 0 || got[0] != "coder-chX" {
		t.Fatalf("coder dest: %v", got)
	}
	if got := turnDestinations("project-manager", "chY"); len(got) == 0 || got[0] != "project-manager-chY" {
		t.Fatalf("pm dest: %v", got)
	}
	if got := turnDestinations("court-persona-ciso", ""); len(got) == 0 || got[0] != "court-persona-ciso" {
		t.Fatalf("court dest: %v", got)
	}
}