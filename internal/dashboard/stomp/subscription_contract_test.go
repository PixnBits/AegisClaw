// Contract tests: STOMP subscription lifecycle per docs/specs/web-portal/test-contracts.md
package stomp_test

import (
	"testing"

	"AegisClaw/internal/dashboard/contracts"
	"AegisClaw/internal/dashboard/testutil"
)

func TestSubscribesToCorrectTopicsOnChannelView(t *testing.T) {
	fake := testutil.NewFakeSTOMPSession()
	defer fake.Close()

	chTopic := contracts.ChannelActivityTopic("chan_1")
	hTopic := contracts.HarnessUpdatesTopic("plan_abc")
	fake.Subscribe(chTopic, "sub-ch")
	fake.Subscribe(hTopic, "sub-h")

	for _, topic := range []string{chTopic, hTopic} {
		fake.Publish(topic, []byte(`{"type":"channel.activity","channel_id":"chan_1","event":{},"timestamp":"t"}`))
		select {
		case frame := <-fake.Sess.Outbound():
			if len(frame) < 7 || frame[:7] != "MESSAGE" {
				t.Fatalf("expected MESSAGE on %s, got %q", topic, frame)
			}
		default:
			t.Fatalf("no MESSAGE delivered for %s", topic)
		}
	}
}

func TestUnsubscribeOnViewNavigation(t *testing.T) {
	fake := testutil.NewFakeSTOMPSession()
	defer fake.Close()

	topic := contracts.ChannelActivityTopic("chan_1")
	fake.Subscribe(topic, "sub-ch")
	fake.Unsubscribe("sub-ch")
	fake.Publish(topic, []byte(`{"ok":true}`))
	select {
	case frame := <-fake.Sess.Outbound():
		t.Fatalf("unexpected frame after unsubscribe: %q", frame)
	default:
	}
}

func TestDisallowedTopicNotSubscribed(t *testing.T) {
	fake := testutil.NewFakeSTOMPSession()
	defer fake.Close()

	fake.Sess.HandleFrame("SUBSCRIBE", map[string]string{
		"id":          "sub-bad",
		"destination": "/topic/evil",
	}, "")
	fake.Publish("/topic/evil", []byte(`{}`))
	select {
	case frame := <-fake.Sess.Outbound():
		t.Fatalf("unexpected delivery on disallowed topic: %q", frame)
	default:
	}
}