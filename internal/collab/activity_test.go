package collab

import "testing"

func TestIsBroadcast(t *testing.T) {
	msg := "Can everyone tell me their name and a short description of what you do?"
	if !IsBroadcast(msg) {
		t.Fatal("expected broadcast hint")
	}
	portal := "Great to meet you all! Can you all tell me one improvement you would make if you had a magic wand?"
	if !IsBroadcast(portal) {
		t.Fatal("expected portal magic-wand prompt to match broadcast hint")
	}
	if IsBroadcast("PM posted a plan step 1") {
		t.Fatal("plan post should not be broadcast hint")
	}
}

func TestIsHumanPoster(t *testing.T) {
	for _, from := range []string{"user", "cli", "web-portal", "portal", "operator"} {
		if !IsHumanPoster(from) {
			t.Fatalf("expected human poster for %q", from)
		}
	}
	if IsHumanPoster("court-persona-ciso") {
		t.Fatal("agent posts should not be human poster")
	}
}

func TestShouldDeliverActivityToAgents(t *testing.T) {
	msg := "Can you all tell me one improvement you would make if you had a magic wand?"
	ok, reason := ShouldRespondToActivity("court-persona-ciso", "operator", msg)
	if !ok || reason != ReasonDelivered {
		t.Fatalf("activity should be delivered to agent, got ok=%v reason=%s", ok, reason)
	}
	ok, reason = ShouldRespondToActivity("court-persona-ciso", "user", "status update")
	if !ok || reason != ReasonDelivered {
		t.Fatalf("non-broadcast human post should still be delivered, got ok=%v reason=%s", ok, reason)
	}
}

func TestPosterIsRole(t *testing.T) {
	if !PosterIsRole("project-manager-party-v2", "project-manager") {
		t.Fatal("PM instance must match project-manager role")
	}
	if PosterIsRole("project-manager-party-v2", "coder") {
		t.Fatal("PM instance must not match coder")
	}
	if !PosterIsRole("coder-tune-css-6", "coder") {
		t.Fatal("coder instance must match coder role")
	}
}

func TestShouldNotRespondToSelf(t *testing.T) {
	ok, _ := ShouldRespondToActivity("court-persona-ciso", "court-persona-ciso", "hello")
	if ok {
		t.Fatal("should ignore self post")
	}
}

func TestIsMentionedOnDemandCoderAndTester(t *testing.T) {
	plan := "Plan for #tune-css-1:\n- @Coder: make the CSS/copy/padding change.\n- @Tester: visual check.\n- No Court."
	if !IsMentioned("coder", plan) {
		t.Fatal("turn recipient role coder must match @Coder in a PM plan")
	}
	if !IsMentioned("coder-tune-css-1", plan) {
		t.Fatal("on-demand coder must match @Coder in a PM plan")
	}
	if !IsMentioned("tester-tune-css-1", plan) {
		t.Fatal("on-demand tester must match @Tester in a PM plan")
	}
	if IsMentioned("coder-tune-css-1", "User: birthday party, no engineering.") {
		t.Fatal("coder must not count as mentioned without @Coder")
	}
}

func TestActivityHints(t *testing.T) {
	b, m := ActivityHints("court-persona-ciso", "hey @court-persona-ciso")
	if !m {
		t.Fatal("expected mention hint")
	}
	if b {
		t.Fatal("did not expect broadcast hint")
	}
}

func TestPayloadContentString(t *testing.T) {
	if got := PayloadContentString("hello"); got != "hello" {
		t.Fatalf("string: %q", got)
	}
	nested := map[string]interface{}{"content": "nested text"}
	if got := PayloadContentString(nested); got != "nested text" {
		t.Fatalf("nested: %q", got)
	}
}

func TestIsCorruptedMapString(t *testing.T) {
	if !IsCorruptedMapString("map[channel_id:main content:foo]") {
		t.Fatal("expected corrupted detection")
	}
}

func TestShouldPMMonitorHumanPost(t *testing.T) {
	if !ShouldPMMonitor("user", "status update please") {
		t.Fatal("PM monitor hint for human posts")
	}
}
