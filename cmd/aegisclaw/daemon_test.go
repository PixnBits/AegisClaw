package main

import (
	"context"
	"strings"
	"testing"

	"github.com/PixnBits/AegisClaw/internal/api"
)

func TestDaemonAPI_Auth(t *testing.T) {
	t.Parallel()

	if err := authorizeCaller(nil, "kernel.shutdown", context.Background()); err == nil {
		t.Fatal("expected missing local caller identity to be rejected")
	} else if !strings.Contains(err.Error(), "authenticated local caller identity") {
		t.Fatalf("unexpected auth error: %v", err)
	}

	if err := authorizeCaller(nil, "kernel.shutdown", api.WithTrustedCaller(context.Background())); err != nil {
		t.Fatalf("trusted caller should be authorized: %v", err)
	}
}
