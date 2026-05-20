//go:build linux

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestServer_UnixPeerAllowRejectsForeignUID(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "aegis", "policy.sock")
	srv := NewServer(socketPath, zap.NewNop())
	srv.UnixPeerAllow = func(uid int) bool { return uid == 999999 }
	srv.Handle("ping", func(context.Context, json.RawMessage) *Response {
		return &Response{Success: true}
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Stop)

	client := NewClient(socketPath)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := client.Call(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Success || !strings.Contains(resp.Error, "not authorized") {
		t.Fatalf("expected peer rejection, got %+v", resp)
	}
}

func TestServer_MaxBodyBytesRejectsLargePayload(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "aegis", "bigbody.sock")
	srv := NewServer(socketPath, zap.NewNop())
	srv.MaxAPIBodyBytes = 64
	srv.Handle("echo", func(context.Context, json.RawMessage) *Response {
		return &Response{Success: true}
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Stop)

	large := strings.Repeat("a", 200)
	body, err := json.Marshal(Request{Action: "echo", Data: json.RawMessage(`{"x":"` + large + `"}`)})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://aegisclaw/api", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
	}
	defer tr.CloseIdleConnections()
	hc := &http.Client{Transport: tr}
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

func TestServer_UnixAPIRateLimit(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "aegis", "rate.sock")
	srv := NewServer(socketPath, zap.NewNop())
	srv.UnixAPIRatePerSec = 3
	srv.Handle("ping", func(context.Context, json.RawMessage) *Response {
		return &Response{Success: true}
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Stop)

	client := NewClient(socketPath)
	ctx := context.Background()
	var last *Response
	for i := 0; i < 5; i++ {
		r, err := client.Call(ctx, "ping", nil)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		last = r
	}
	if last == nil || last.Success || !strings.Contains(last.Error, "rate limit") {
		t.Fatalf("expected rate limit on last call, got %+v", last)
	}
}
