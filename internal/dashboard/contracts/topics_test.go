package contracts

import "testing"

func TestIsAllowedTopic(t *testing.T) {
	cases := []struct {
		topic string
		ok    bool
	}{
		{TopicOverviewStats, true},
		{ChannelActivityTopic("main"), true},
		{LegacyChannelMessagesTopic("main"), true},
		{HarnessUpdatesTopic("plan_1"), true},
		{ConversationUpdatesTopic("sess_1"), true},
		{ProposalUpdatesTopic("prop_1"), true},
		{LLMUsageTopic(""), true},
		{LLMUsageTopic("agent-foo"), true},
		{"/topic/evil.hacks", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsAllowedTopic(tc.topic); got != tc.ok {
			t.Errorf("IsAllowedTopic(%q) = %v, want %v", tc.topic, got, tc.ok)
		}
	}
}

func TestTopicsForViewChannels(t *testing.T) {
	topics := TopicsForView(ViewChannels, ViewContext{ChannelID: "main", PlanID: "plan_abc"})
	if len(topics) != 2 {
		t.Fatalf("topics: %v", topics)
	}
	if topics[0] != ChannelActivityTopic("main") {
		t.Fatalf("channel topic: %s", topics[0])
	}
	if topics[1] != HarnessUpdatesTopic("plan_abc") {
		t.Fatalf("harness topic: %s", topics[1])
	}
}