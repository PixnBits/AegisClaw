package sanitize

import "testing"

func TestRestoreChannelIDField(t *testing.T) {
	cases := []struct {
		raw, clean, want string
	}{
		{`{"channel_id":"task-refactorauthenticationmodule","x":"a"}`, `{"channel_id":"ta[REDACTED]","x":"b"}`, `{"channel_id":"task-refactorauthenticationmodule","x":"b"}`},
		{`{"channel_id":"Bad_ID"}`, `{"channel_id":"c"}`, `{"channel_id":"c"}`},
		{`{"other":"plan-demo"}`, `{"other":"c"}`, `{"other":"c"}`},
		{`["plan-demo"]`, `["c"]`, `["c"]`},
		{`{"channel_id":"plan-demo"}`, `not json`, `not json`},
		{`{"channel_id":7}`, `{"channel_id":7}`, `{"channel_id":7}`},
	}
	for i, tc := range cases {
		if got := string(RestoreChannelIDField([]byte(tc.raw), []byte(tc.clean), "channel_id")); got != tc.want {
			t.Errorf("case %d: got %s, want %s", i, got, tc.want)
		}
	}
}
