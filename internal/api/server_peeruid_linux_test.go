//go:build linux

package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestUnixSocketRequestCarriesPeerUID(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "daemon.sock")
	srv := NewServer(socketPath, zap.NewNop())
	srv.Handle("peer.uid", func(ctx context.Context, _ json.RawMessage) *Response {
		uid, ok := PeerUIDFromContext(ctx)
		raw, _ := json.Marshal(map[string]any{"uid": uid, "ok": ok})
		return &Response{Success: true, Data: raw}
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Stop)

	client := NewClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := client.Call(ctx, "peer.uid", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !resp.Success {
		t.Fatalf("call failed: %s", resp.Error)
	}
	var payload struct {
		UID int  `json:"uid"`
		OK  bool `json:"ok"`
	}
	if err := json.Unmarshal(resp.Data, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !payload.OK {
		t.Fatal("expected peer UID in request context")
	}
	if payload.UID != os.Geteuid() {
		t.Fatalf("expected uid %d, got %d", os.Geteuid(), payload.UID)
	}
}
