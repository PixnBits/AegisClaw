package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
)

// statusAPIClient returns either a Call error or a fixed APIResponse.
type statusAPIClient struct {
	err  error
	resp *APIResponse
}

func (c statusAPIClient) Call(context.Context, string, json.RawMessage) (*APIResponse, error) {
	if c.err != nil {
		return nil, c.err
	}
	if c.resp != nil {
		return c.resp, nil
	}
	return &APIResponse{Success: false, Error: "unspecified"}, nil
}

func postGoal(t *testing.T, client APIClient) *httptest.ResponseRecorder {
	t.Helper()
	srv, err := New("127.0.0.1:0", client)
	if err != nil {
		t.Fatal(err)
	}
	req := httptestRequest(t, http.MethodPost, "/api/goals", strings.NewReader(`{"goal":"ship","channel_id":"main"}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestGoalSubmitStatusClasses(t *testing.T) {
	secret := "sk-abcdefghijklmnopqrstuvwxyz"
	cases := []struct {
		name       string
		client     APIClient
		wantStatus int
		wantBody   string
		rejectBody string
	}{
		{
			name: "upstream reply",
			client: statusAPIClient{resp: &APIResponse{
				Success: false,
				Error:   "goal.submit: ensure PM for main: invalid vm id: refused",
			}},
			wantStatus: http.StatusBadGateway,
			wantBody:   "invalid vm id",
		},
		{
			name:       "connection refused",
			client:     statusAPIClient{err: fmt.Errorf("dial hub: %w", syscall.ECONNREFUSED)},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "connection refused",
		},
		{
			name: "net op refused",
			client: statusAPIClient{err: &net.OpError{
				Op:  "dial",
				Net: "unix",
				Err: syscall.ECONNREFUSED,
			}},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "connection refused",
		},
		{
			name:       "missing socket",
			client:     statusAPIClient{err: fmt.Errorf("dial unix: %w", os.ErrNotExist)},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "file does not exist",
		},
		{
			name:       "deadline",
			client:     statusAPIClient{err: fmt.Errorf("failed to receive response for goal.submit: %w", context.DeadlineExceeded)},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "deadline exceeded",
		},
		{
			name: "net timeout",
			client: statusAPIClient{err: &net.OpError{
				Op:  "dial",
				Net: "unix",
				Err: os.ErrDeadlineExceeded,
			}},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "typed unavailable",
			client:     statusAPIClient{err: &UnavailableError{Err: fmt.Errorf("web-portal: no live daemon connection")}},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "no live daemon connection",
		},
		{
			name:       "plain dial failure",
			client:     statusAPIClient{err: fmt.Errorf("bridge session closed")},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "bridge session closed",
		},
		{
			name: "sanitized upstream",
			client: statusAPIClient{resp: &APIResponse{
				Success: false,
				Error:   "goal failed " + secret,
			}},
			wantStatus: http.StatusBadGateway,
			wantBody:   "[REDACTED]",
			rejectBody: secret,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postGoal(t, tc.client)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
			}
			if rec.Code == http.StatusBadRequest {
				t.Fatal("fetch error was classified as 400")
			}
			if strings.Contains(rec.Body.String(), `"preview"`) {
				t.Fatalf("error still previewed: %s", rec.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("body %q does not contain %q", rec.Body.String(), tc.wantBody)
			}
			if tc.rejectBody != "" && strings.Contains(rec.Body.String(), tc.rejectBody) {
				t.Fatalf("body leaked %q: %s", tc.rejectBody, rec.Body.String())
			}
		})
	}

	// A bare net timeout (not wrapped in UnavailableError) is still 503, not 502.
	timeout := &net.OpError{Op: "read", Net: "unix", Err: timeoutErr{}}
	if goalSubmitStatus(timeout) != http.StatusServiceUnavailable {
		t.Fatalf("net timeout status %d", goalSubmitStatus(timeout))
	}
}

// timeoutErr is a net.Error that is not context.DeadlineExceeded.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// TestGoalSubmitStatusBareErrors covers errors that reach goalSubmitStatus
// without fetchRaw's UnavailableError wrapper (bridgeGuard, future callers).
func TestGoalSubmitStatusBareErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"deadline", fmt.Errorf("goal.submit: %w", context.DeadlineExceeded), http.StatusServiceUnavailable},
		{"canceled", fmt.Errorf("goal.submit: %w", context.Canceled), http.StatusServiceUnavailable},
		{"econnrefused", fmt.Errorf("dial: %w", syscall.ECONNREFUSED), http.StatusServiceUnavailable},
		{"missing socket", fmt.Errorf("dial: %w", os.ErrNotExist), http.StatusServiceUnavailable},
		{"upstream reply", &UpstreamError{Msg: "store refused"}, http.StatusBadGateway},
		{"other", errors.New("bridge: action not allowed"), http.StatusBadGateway},
	}
	for _, tc := range cases {
		if got := goalSubmitStatus(tc.err); got != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, got, tc.want)
		}
	}
}
