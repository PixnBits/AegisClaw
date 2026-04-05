package api

import (
	"context"
	"encoding/json"
	"testing"

	"go.uber.org/zap"
)

func TestCallDirectRecoversFromHandlerPanic(t *testing.T) {
	srv := NewServer("", zap.NewNop())
	srv.Handle("panic.action", func(context.Context, json.RawMessage) *Response {
		panic("boom")
	})

	resp := srv.CallDirect(context.Background(), "panic.action", nil)
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if resp.Success {
		t.Fatal("expected recovered panic to return unsuccessful response")
	}
	if resp.Error != "internal handler panic" {
		t.Fatalf("expected internal handler panic error, got %q", resp.Error)
	}
}