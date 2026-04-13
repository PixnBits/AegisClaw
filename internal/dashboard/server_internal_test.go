package dashboard

import "testing"

func TestSuppressInFlightStructuredContent(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain prose preserved",
			in:   "I have submitted the proposal for review.",
			want: "I have submitted the proposal for review.",
		},
		{
			name: "raw json suppressed",
			in:   `{"name":"proposal.submit","args":{"id":"abc"}}`,
			want: "",
		},
		{
			name: "fenced tool call suppressed",
			in:   "```tool-call\n{\"name\":\"proposal.submit\",\"args\":{\"id\":\"abc\"}}\n```",
			want: "",
		},
		{
			name: "fenced json suppressed",
			in:   "```json\n{\"status\":\"tool_call\",\"tool\":\"proposal.submit\"}\n```",
			want: "",
		},
		{
			name: "leading whitespace structured suppressed",
			in:   "  \n {\"status\":\"tool_call\"}",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := suppressInFlightStructuredContent(tc.in); got != tc.want {
				t.Fatalf("suppressInFlightStructuredContent(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}