package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Fake key material for tests; not real credentials.
var (
	detailSecret = "sk-" + strings.Repeat("Qw7", 8)
	// Valid channel ids that the credential pattern matches in part. The first
	// two both redact to exactly "[REDACTED]".
	keyLikeA = "sk-abcdefghijklmnopqrstu"
	keyLikeB = "sk-zyxwvutsrqponmlkjihgf"
	refactor = "task-refactorauthenticationmodule"
	glued    = "plan2sk-abcdefghijklmnopqrstuvw"
	akiaLike = "akiaabcdefghijklmnop"
	badKeyID = "SK-ABCDEFGHIJKLMNOPQRSTU" // invalid id (uppercase), key-shaped
)

func serveJSON(t *testing.T, data map[string]interface{}, path string) (interface{}, string) {
	t.Helper()
	srv, err := New("127.0.0.1:0", &recordAPIClient{data: data})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptestRequest(t, http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d body %s", path, rec.Code, rec.Body.String())
	}
	var v interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v, rec.Body.String()
}

func getChannelDetail(t *testing.T, detail interface{}) (map[string]interface{}, string) {
	t.Helper()
	v, body := serveJSON(t, map[string]interface{}{"channel.get": detail}, "/api/channels/x")
	m, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("detail body %s", body)
	}
	return m, body
}

func TestChannelDetailShowsValidIDRaw(t *testing.T) {
	for _, id := range []string{refactor, keyLikeA, glued, akiaLike, "plan-demo"} {
		got, body := getChannelDetail(t, map[string]interface{}{
			"id":    id,
			"name":  "name " + detailSecret,
			"topic": "topic " + detailSecret,
			"messages": []interface{}{
				map[string]interface{}{"channel_id": id, "from": "user", "content": "hi " + detailSecret},
			},
			"members": []interface{}{map[string]interface{}{"role": "coder", "agent_id": detailSecret}},
		})
		if got["id"] != id {
			t.Errorf("detail id = %v, want %q", got["id"], id)
		}
		msgs, _ := got["messages"].([]interface{})
		if len(msgs) != 1 {
			t.Fatalf("messages %#v", got["messages"])
		}
		m := msgs[0].(map[string]interface{})
		if m["channel_id"] != id {
			t.Errorf("messages[0].channel_id = %v, want %q", m["channel_id"], id)
		}
		if strings.Contains(body, detailSecret) || strings.Contains(body, detailSecret[3:]) {
			t.Errorf("detail for %q leaks the secret: %s", id, body)
		}
		for _, f := range []string{"name", "topic"} {
			if s, _ := got[f].(string); !strings.Contains(s, "[REDACTED]") {
				t.Errorf("%s = %q, want redacted", f, s)
			}
		}
		if c, _ := m["content"].(string); !strings.Contains(c, "[REDACTED]") {
			t.Errorf("content = %q, want redacted", c)
		}
	}
}

func TestChannelDetailInvalidIDStaysRedacted(t *testing.T) {
	got, body := getChannelDetail(t, map[string]interface{}{
		"id":       badKeyID,
		"messages": []interface{}{map[string]interface{}{"channel_id": badKeyID, "content": "x"}},
	})
	if got["id"] != "[REDACTED]" {
		t.Errorf("invalid key-shaped id = %v, want [REDACTED]", got["id"])
	}
	if strings.Contains(body, badKeyID[3:]) {
		t.Errorf("invalid id body leaked: %s", body)
	}
	m := got["messages"].([]interface{})[0].(map[string]interface{})
	if m["channel_id"] != "[REDACTED]" {
		t.Errorf("invalid message channel_id = %v", m["channel_id"])
	}
}

// Messages are matched by position: two valid ids that redact to the same
// text each get their own raw id back, and an invalid one in between stays
// redacted.
func TestChannelDetailMessageIDsByPosition(t *testing.T) {
	got, _ := getChannelDetail(t, map[string]interface{}{
		"id": keyLikeA,
		"messages": []interface{}{
			map[string]interface{}{"channel_id": keyLikeB, "content": "b"},
			"not-an-object " + detailSecret,
			map[string]interface{}{"channel_id": badKeyID, "content": "bad"},
			map[string]interface{}{"channel_id": keyLikeA, "content": "a"},
		},
	})
	msgs := got["messages"].([]interface{})
	if len(msgs) != 4 {
		t.Fatalf("messages %#v", msgs)
	}
	want := []interface{}{keyLikeB, nil, "[REDACTED]", keyLikeA}
	for i, w := range want {
		m, ok := msgs[i].(map[string]interface{})
		if w == nil {
			if s, _ := msgs[i].(string); ok || !strings.Contains(s, "[REDACTED]") {
				t.Errorf("messages[%d] = %#v, want a redacted string", i, msgs[i])
			}
			continue
		}
		if !ok || m["channel_id"] != w {
			t.Errorf("messages[%d].channel_id = %#v, want %v", i, msgs[i], w)
		}
	}
	if got["id"] != keyLikeA {
		t.Errorf("id = %v", got["id"])
	}
}

// A payload that isn't an object is still sanitized whole.
func TestChannelDetailNonObjectSanitized(t *testing.T) {
	_, body := serveJSON(t, map[string]interface{}{"channel.get": []interface{}{"leak " + detailSecret, refactor}}, "/api/channels/x")
	if strings.Contains(body, detailSecret) || strings.Contains(body, detailSecret[3:]) {
		t.Fatalf("non-object detail leaked: %s", body)
	}
	if strings.Contains(body, refactor) {
		t.Fatalf("non-object detail restored an id: %s", body)
	}
}

// List entries are matched by position (X2): two valid ids that both redact
// to "[REDACTED]" each display their own raw id.
func TestChannelListIDsByPosition(t *testing.T) {
	chans, _ := getChannelList(t, []interface{}{
		map[string]interface{}{"id": keyLikeB},
		map[string]interface{}{"id": badKeyID},
		map[string]interface{}{"id": keyLikeA},
	})
	want := []string{keyLikeB, "[REDACTED]", keyLikeA}
	for i, w := range want {
		if chans[i]["id"] != w {
			t.Errorf("channels[%d].id = %v, want %q", i, chans[i]["id"], w)
		}
	}
}

// Only the id is restored (X9): a secret in name or description next to a
// valid id stays redacted.
func TestChannelListRestoresOnlyID(t *testing.T) {
	chans, body := getChannelList(t, []interface{}{
		map[string]interface{}{"id": refactor, "name": detailSecret, "description": "d " + detailSecret},
	})
	if chans[0]["id"] != refactor {
		t.Errorf("id = %v", chans[0]["id"])
	}
	if strings.Contains(body, detailSecret) || strings.Contains(body, detailSecret[3:]) {
		t.Fatalf("list leaked name/description secret: %s", body)
	}
	if chans[0]["name"] != "[REDACTED]" {
		t.Errorf("name = %v, want [REDACTED]", chans[0]["name"])
	}
}

// A channel.list payload that isn't a list is still sanitized (X10).
func TestChannelListNonListSanitized(t *testing.T) {
	v, body := serveJSON(t, map[string]interface{}{"channel.list": map[string]interface{}{"id": refactor, "note": "x " + detailSecret}}, "/api/channels")
	if strings.Contains(body, detailSecret) || strings.Contains(body, detailSecret[3:]) {
		t.Fatalf("non-list channel.list leaked: %s", body)
	}
	m, _ := v.(map[string]interface{})
	inner, _ := m["channels"].(map[string]interface{})
	if inner == nil || inner["id"] == refactor {
		t.Fatalf("non-list channel.list = %s, want sanitized object with no id restore", body)
	}
}
