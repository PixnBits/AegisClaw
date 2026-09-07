package main

import (
	"testing"

	"AegisClaw/internal/transport/hubclient"
)

func TestHubDialPlanUnixWhenSocketSet(t *testing.T) {
	plan := hubDialPlanFromEnv("/tmp/hub.sock")
	if plan.kind != hubDialUnix {
		t.Fatalf("AEGIS_HUB_SOCKET set: kind=%v want unix", plan.kind)
	}
	if plan.unixSock != "/tmp/hub.sock" {
		t.Fatalf("unixSock=%q", plan.unixSock)
	}
}

func TestHubDialPlanVsockWhenSocketUnset(t *testing.T) {
	plan := hubDialPlanFromEnv("")
	if plan.kind != hubDialVsock {
		t.Fatal("empty AEGIS_HUB_SOCKET must select vsock, not require the env")
	}
	if plan.unixSock != "" {
		t.Fatalf("vsock plan must not keep a unix path, got %q", plan.unixSock)
	}
	if plan.vsockCID != hubclient.HostCID || plan.vsockPort != hubclient.HubVsockPort {
		t.Fatalf("vsock cid:port=%d:%d want %d:%d", plan.vsockCID, plan.vsockPort, hubclient.HostCID, hubclient.HubVsockPort)
	}
}

func TestHubDialPlanWhitespaceSocketIsVsock(t *testing.T) {
	plan := hubDialPlanFromEnv("   ")
	if plan.kind != hubDialVsock {
		t.Fatal("whitespace AEGIS_HUB_SOCKET must select vsock")
	}
}
