package hubids

import "testing"

func TestFamilies(t *testing.T) {
	for _, id := range []string{"aegis-daemon-temp", "aegis-daemon-temp-x", "daemon-temp-1", "aegis-cli-internal", "aegis-cli-internal-42"} {
		if !IsEphemeralClient(id) || !IsHostClient(id) {
			t.Errorf("%q: ephemeral=%v host=%v, want both", id, IsEphemeralClient(id), IsHostClient(id))
		}
	}
	for _, id := range []string{"daemon", "daemon-internal-1", "channel-facilitator-out-1"} {
		if IsEphemeralClient(id) || !IsHostClient(id) {
			t.Errorf("%q: ephemeral=%v host=%v, want host only", id, IsEphemeralClient(id), IsHostClient(id))
		}
	}
	for _, id := range []string{"aegis-daemon-tempx", "aegis-cli-internalx", "aegis-daemon-temp-", "daemon-temp", "coder-1", ""} {
		if id != "daemon-temp" && IsEphemeralClient(id) {
			t.Errorf("IsEphemeralClient(%q) = true", id)
		}
	}
	for _, id := range []string{"aegis-daemon-tempx", "aegis-cli-internalx", "daemonx", "coder-1", "", "daemon-"} {
		if IsHostClient(id) {
			t.Errorf("IsHostClient(%q) = true", id)
		}
	}
}
