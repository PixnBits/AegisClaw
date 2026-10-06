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

func TestChannelListAnnotatesInvalidIDs(t *testing.T) {
	client := &recordAPIClient{data: map[string]interface{}{
		"channel.list": []interface{}{
			map[string]interface{}{"id": "main", "members": []interface{}{}},
			map[string]interface{}{"id": "MyProj"},
			map[string]interface{}{"id": "q4_plan"},
		},
	}}
	srv, err := New("127.0.0.1:0", client)
	if err != nil {
		t.Fatal(err)
	}
	req := httptestRequest(t, http.MethodGet, "/api/channels", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Channels []map[string]interface{} `json:"channels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Channels) != 3 {
		t.Fatalf("channels %#v", body.Channels)
	}
	if _, ok := body.Channels[0]["id_valid"]; ok {
		t.Fatalf("valid channel annotated: %#v", body.Channels[0])
	}
	for _, ch := range body.Channels[1:] {
		if ch["id_valid"] != false {
			t.Fatalf("id %v valid flag %#v", ch["id"], ch["id_valid"])
		}
		msg, _ := ch["id_error"].(string)
		if !strings.Contains(msg, "invalid channel id") || !strings.Contains(msg, "<= 45 chars") {
			t.Fatalf("id %v error %q", ch["id"], msg)
		}
	}
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
	if rec.Code != http.StatusBadGateway {
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

// TestChannelListValidIDNotRedactedIntoInvalid: the id check runs before
// redaction. "task-refactorauthenticationmodule" contains "sk-" followed by
// 20+ letters, which the credential pattern used to rewrite to
// "ta[REDACTED]"; checked after that, a valid id got the invalid badge.
func TestChannelListValidIDNotRedactedIntoInvalid(t *testing.T) {
	const id = "task-refactorauthenticationmodule"
	client := &recordAPIClient{data: map[string]interface{}{
		"channel.list": []interface{}{
			map[string]interface{}{"id": id, "members": []interface{}{}},
		},
	}}
	srv, err := New("127.0.0.1:0", client)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptestRequest(t, http.MethodGet, "/api/channels", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Channels []map[string]interface{} `json:"channels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Channels) != 1 {
		t.Fatalf("channels %#v", body.Channels)
	}
	ch := body.Channels[0]
	if _, ok := ch["id_valid"]; ok {
		t.Fatalf("valid id %q got the invalid badge: %#v", id, ch)
	}
	if ch["id"] != id {
		t.Fatalf("id rewritten to %#v", ch["id"])
	}
}

// TestChannelListAnnotatesBeforeRedaction pins the order. "sk-" + 21
// letters is a valid channel id that the credential pattern still redacts
// for display. The badge must reflect the raw id, not "[REDACTED]".
func getChannelList(t *testing.T, list []interface{}) ([]map[string]interface{}, string) {
	t.Helper()
	client := &recordAPIClient{data: map[string]interface{}{"channel.list": list}}
	srv, err := New("127.0.0.1:0", client)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptestRequest(t, http.MethodGet, "/api/channels", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Channels []map[string]interface{} `json:"channels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Channels) != len(list) {
		t.Fatalf("channels %#v", body.Channels)
	}
	return body.Channels, rec.Body.String()
}

// Valid ids are checked raw and displayed raw, even when they look like a
// key; invalid ids and every other field are still redacted.
func TestChannelListAnnotatesBeforeRedaction(t *testing.T) {
	keyLike := "sk-abcdefghijklmnopqrstu"      // valid id shaped like a key
	glued := "plan2sk-abcdefghijklmnopqrstuvw" // valid id, key glued to a digit
	badKey := "SK-ABCDEFGHIJKLMNOPQRSTU"       // invalid (uppercase), key-shaped
	secret := "sk-" + strings.Repeat("Zx9", 8)
	chans, raw := getChannelList(t, []interface{}{
		map[string]interface{}{"id": keyLike, "topic": "api_key_" + secret},
		map[string]interface{}{"id": glued},
		map[string]interface{}{"id": badKey},
		map[string]interface{}{"id": "main", "note": "k%3D" + secret},
	})
	for i, want := range []string{keyLike, glued} {
		if _, ok := chans[i]["id_valid"]; ok {
			t.Fatalf("valid id %q got the invalid badge: %#v", want, chans[i])
		}
		if chans[i]["id"] != want {
			t.Fatalf("valid id displayed as %#v, want %q", chans[i]["id"], want)
		}
	}
	if chans[2]["id_valid"] != false {
		t.Fatalf("invalid id has no badge: %#v", chans[2])
	}
	if strings.Contains(raw, badKey[3:]) {
		t.Fatalf("invalid key-shaped id reached the browser unredacted: %s", raw)
	}
	if strings.Contains(raw, secret[3:]) {
		t.Fatalf("a key in another field of a valid channel leaked: %s", raw)
	}
	if chans[0]["topic"] != "api_key_[REDACTED]" || chans[3]["note"] != "k%3D[REDACTED]" {
		t.Fatalf("other fields not redacted: %#v / %#v", chans[0]["topic"], chans[3]["note"])
	}
}

// Scoped-key redaction (#152) must not touch valid channel ids: they are
// shown raw, and in other fields a scoped key is still redacted.
func TestChannelListScopedPrefixIDDisplaysRaw(t *testing.T) {
	const id = "sk-proj-roadmap-planning-notes-q4"
	key := "sk-" + "proj-" + "AbC_12-" + strings.Repeat("Xy9_Zq-0", 4)
	chans, raw := getChannelList(t, []interface{}{
		map[string]interface{}{"id": id, "topic": "key " + key},
	})
	if chans[0]["id"] != id {
		t.Fatalf("valid id displayed as %#v", chans[0]["id"])
	}
	if _, ok := chans[0]["id_valid"]; ok {
		t.Fatalf("valid id got the badge: %#v", chans[0])
	}
	if strings.Contains(raw, key[8:]) || chans[0]["topic"] != "key [REDACTED]" {
		t.Fatalf("scoped key in topic not redacted: %s", raw)
	}
}
