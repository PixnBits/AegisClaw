package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type recordAPIClient struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]string
	data  map[string]interface{}
}

func (c *recordAPIClient) Call(_ context.Context, action string, _ json.RawMessage) (*APIResponse, error) {
	c.mu.Lock()
	c.calls = append(c.calls, action)
	c.mu.Unlock()
	if msg, ok := c.fail[action]; ok {
		return &APIResponse{Success: false, Error: msg}, nil
	}
	payload := c.data[action]
	if payload == nil {
		payload = map[string]interface{}{"id": "ok"}
	}
	b, _ := json.Marshal(payload)
	return &APIResponse{Success: true, Data: b}, nil
}

func (c *recordAPIClient) called(action string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, got := range c.calls {
		if got == action {
			return true
		}
	}
	return false
}

func TestSPACreateChannelIDRule(t *testing.T) {
	reject := []string{"MyProj", "q4_plan", "v1.2", strings.Repeat("a", 46), ""}
	for _, id := range reject {
		client := &recordAPIClient{}
		srv, err := New("127.0.0.1:0", client)
		if err != nil {
			t.Fatal(err)
		}
		body := `{"id":"` + id + `"}`
		req := httptestRequest(t, http.MethodPost, "/api/channels", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("id %q: status %d body %s", id, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "invalid channel id") || !strings.Contains(rec.Body.String(), "<= 45 chars") {
			t.Fatalf("id %q: body %s", id, rec.Body.String())
		}
		if client.called("channel.create") {
			t.Fatalf("id %q reached the store", id)
		}
	}

	accept := []string{"main", "plan-demo", strings.Repeat("a", 45)}
	for _, id := range accept {
		client := &recordAPIClient{}
		srv, err := New("127.0.0.1:0", client)
		if err != nil {
			t.Fatal(err)
		}
		req := httptestRequest(t, http.MethodPost, "/api/channels", strings.NewReader(`{"id":"`+id+`"}`))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("id %q: status %d body %s", id, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), id) {
			t.Fatalf("id %q: body %s", id, rec.Body.String())
		}
		if !client.called("channel.create") {
			t.Fatalf("id %q did not call channel.create", id)
		}
	}
}

func TestGoalSubmitChannelErrors(t *testing.T) {
	reject := []string{"MyProj", "q4_plan", "v1.2", strings.Repeat("a", 46)}
	for _, id := range reject {
		client := &recordAPIClient{}
		srv, err := New("127.0.0.1:0", client)
		if err != nil {
			t.Fatal(err)
		}
		req := httptestRequest(t, http.MethodPost, "/api/goals", strings.NewReader(`{"goal":"ship","channel_id":"`+id+`"}`))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("id %q: status %d body %s", id, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "invalid channel id") {
			t.Fatalf("id %q: body %s", id, rec.Body.String())
		}
		if client.called("goal.submit") {
			t.Fatalf("id %q was submitted", id)
		}
	}

	client := &recordAPIClient{fail: map[string]string{
		"goal.submit": "goal.submit: ensure PM for main: invalid vm id: refused",
	}}
	srv, err := New("127.0.0.1:0", client)
	if err != nil {
		t.Fatal(err)
	}
	req := httptestRequest(t, http.MethodPost, "/api/goals", strings.NewReader(`{"goal":"ship","channel_id":"main"}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ensure refusal: status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid vm id") {
		t.Fatalf("ensure error not surfaced: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"preview":true`) || strings.Contains(rec.Body.String(), `"preview": true`) {
		t.Fatalf("refused goal still previewed: %s", rec.Body.String())
	}

	okClient := &recordAPIClient{data: map[string]interface{}{
		"goal.submit": map[string]interface{}{"plan_id": "plan_main", "channel_id": "main", "status": "accepted"},
	}}
	okSrv, err := New("127.0.0.1:0", okClient)
	if err != nil {
		t.Fatal(err)
	}
	okReq := httptestRequest(t, http.MethodPost, "/api/goals", strings.NewReader(`{"goal":"ship"}`))
	okRec := httptest.NewRecorder()
	okSrv.ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK {
		t.Fatalf("success status %d body %s", okRec.Code, okRec.Body.String())
	}
	if !strings.Contains(okRec.Body.String(), `"preview":true`) && !strings.Contains(okRec.Body.String(), `"preview": true`) {
		t.Fatalf("success body %s", okRec.Body.String())
	}
	if !strings.Contains(okRec.Body.String(), "plan_main") {
		t.Fatalf("success body %s", okRec.Body.String())
	}
}
