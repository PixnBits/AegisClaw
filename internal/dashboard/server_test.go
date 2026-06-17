package dashboard

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeAPIClient struct {
	data map[string]interface{} // action -> raw data value (will be json marshaled into Data)
}

func (f *fakeAPIClient) Call(ctx context.Context, action string, payload json.RawMessage) (*APIResponse, error) {
	if f.data == nil {
		return &APIResponse{Success: false, Error: "no data"}, nil
	}
	if d, ok := f.data[action]; ok {
		b, _ := json.Marshal(d)
		return &APIResponse{Success: true, Data: b}, nil
	}
	// For actions not provided, return empty success so handlers don't get hard errors
	// (most handle* ignore fetch errors anyway).
	b, _ := json.Marshal([]interface{}{})
	return &APIResponse{Success: true, Data: b}, nil
}

// TestSafeWorkersList exercises the normalizer that prevents template crashes
// when "worker.list" returns non-slice or lists with non-map entries (the
// historical source of "template exec error ... at <"worker_id">: invalid value; expected string").
func TestSafeWorkersList(t *testing.T) {
	cases := []struct {
		name     string
		in       interface{}
		wantNil  bool
		wantLen  int
		wantID   string // if len>0, check first item's "id" survived
	}{
		{"nil", nil, true, 0, ""},
		{"empty slice", []interface{}{}, false, 0, ""},
		{"real portalWorkerList shape", []interface{}{
			map[string]interface{}{"id": "agent-123", "name": "foo", "status": "running", "role": "agent"},
		}, false, 1, "agent-123"},
		{"legacy worker_id shape", []interface{}{
			map[string]interface{}{"worker_id": "w-xyz", "role": "researcher"},
		}, false, 1, ""}, // id may be absent, but item kept
		{"map instead of list (bad stub/bridge)", map[string]interface{}{"foo": "bar"}, true, 0, ""},
		{"mixed list with scalars and good map", []interface{}{
			"not-a-map",
			42,
			map[string]interface{}{"id": "only-good", "status": "idle"},
			nil,
		}, false, 1, "only-good"},
		{"slice of non-maps only", []interface{}{"a", 1, true}, false, 0, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := safeWorkersList(tc.in)
			if tc.wantNil {
				if out != nil {
					t.Errorf("expected nil, got %#v", out)
				}
				return
			}
			list, ok := out.([]interface{})
			if !ok {
				t.Fatalf("expected []interface{}, got %T", out)
			}
			if len(list) != tc.wantLen {
				t.Errorf("len=%d want %d", len(list), tc.wantLen)
			}
			if tc.wantLen > 0 && tc.wantID != "" {
				if m, ok := list[0].(map[string]interface{}); ok {
					if got, _ := m["id"].(string); got != tc.wantID {
						t.Errorf("first id=%q want %q", got, tc.wantID)
					}
				}
			}
		})
	}
}

// TestOverviewRendersWithWeirdWorkers ensures handleIndex (the path for GET /)
// never returns a template exec error (500 with "template exec error") no
// matter what shape "worker.list" returns. This covers the exact failure mode
// reported with line 97:36 / "worker_id".
func TestOverviewRendersWithWeirdWorkers(t *testing.T) {
	weirdCases := []struct {
		name    string
		workers interface{}
	}{
		{"nil", nil},
		{"empty", []interface{}{}},
		{"good agent maps", []interface{}{
			map[string]interface{}{"id": "agent-research", "name": "researcher", "status": "running", "role": "agent", "task_description": "foo"},
		}},
		{"maps without worker_id or id (old partial data)", []interface{}{
			map[string]interface{}{"name": "mystery", "status": "idle", "role": "agent"},
		}},
		{"non-slice (object)", map[string]interface{}{"unexpected": "shape"}},
		{"list with junk entries", []interface{}{"string", 99, map[string]interface{}{"id": "survivor"}}},
	}

	for _, tc := range weirdCases {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakeAPIClient{data: map[string]interface{}{
				"worker.list":              tc.workers,
				"event.approvals.list":       []interface{}{},
				"event.timers.list":          []interface{}{},
				"sandbox.list":               []interface{}{},
				"memory.list":                map[string]interface{}{"total": float64(0)},
				"system.stats":               map[string]interface{}{},
			}}
			srv, err := New("127.0.0.1:0", fc)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			req := httptest.NewRequest("GET", "/", nil)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			body := rec.Body.String()
			if rec.Code != 200 {
				t.Errorf("status=%d body=%s", rec.Code, body)
			}
			if strings.Contains(body, "template exec error") || strings.Contains(body, "invalid value") {
				t.Errorf("page contained template error: %s", body)
			}
			// Basic smoke that we got the overview chrome
			if !strings.Contains(body, "Overview") && !strings.Contains(body, "Running MicroVMs") {
				t.Errorf("unexpected body (no overview markers): %s", body[:min(200, len(body))])
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestAgentsRendersWithWeirdWorkers is the parallel for the /agents page.
func TestAgentsRendersWithWeirdWorkers(t *testing.T) {
	fc := &fakeAPIClient{data: map[string]interface{}{
		"worker.list": []interface{}{
			map[string]interface{}{"id": "a1"},
			"junk",
			map[string]interface{}{"worker_id": "legacy"},
		},
	}}
	srv, _ := New("127.0.0.1:0", fc)

	req := httptest.NewRequest("GET", "/agents", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "template exec error") {
		t.Error("agents page had template exec error")
	}
}
