package sandbox

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func TestBuildFirecrackerConfig_NoNetworkOmitsInterfaceAndIPArgs(t *testing.T) {
	rt := &FirecrackerRuntime{cfg: RuntimeConfig{KernelImage: "/kernel"}}
	spec := SandboxSpec{
		ID:   "skill-default-script-runner",
		Name: "skill-default-script-runner",
		Resources: Resources{
			VCPUs:    1,
			MemoryMB: 256,
		},
		NetworkPolicy: NetworkPolicy{
			NoNetwork:   true,
			DefaultDeny: true,
		},
		VsockCID:   42,
		RootfsPath: "/rootfs.ext4",
	}

	cfg := rt.buildFirecrackerConfig(spec, "/state/api.sock", "/rootfs.ext4", "/workspace.ext4", "", "", "")
	if len(cfg.NetworkInterfaces) != 0 {
		t.Fatalf("expected no network interfaces, got %d", len(cfg.NetworkInterfaces))
	}
	if strings.Contains(cfg.KernelArgs, " ip=") {
		t.Fatalf("kernel args unexpectedly contain IP configuration: %q", cfg.KernelArgs)
	}
}

func TestBuildFirecrackerConfig_WithNetworkAddsInterfaceAndIPArgs(t *testing.T) {
	rt := &FirecrackerRuntime{cfg: RuntimeConfig{KernelImage: "/kernel"}}
	spec := SandboxSpec{
		ID:   "skill-networked",
		Name: "skill-networked",
		Resources: Resources{
			VCPUs:    1,
			MemoryMB: 256,
		},
		NetworkPolicy: NetworkPolicy{
			DefaultDeny: true,
		},
		VsockCID:   43,
		RootfsPath: "/rootfs.ext4",
	}

	cfg := rt.buildFirecrackerConfig(spec, "/state/api.sock", "/rootfs.ext4", "/workspace.ext4", "tap0", "10.0.0.2", "10.0.0.1")
	if len(cfg.NetworkInterfaces) != 1 {
		t.Fatalf("expected one network interface, got %d", len(cfg.NetworkInterfaces))
	}
	if !strings.Contains(cfg.KernelArgs, "ip=10.0.0.2::10.0.0.1:255.255.255.252::eth0:off") {
		t.Fatalf("kernel args missing expected IP config: %q", cfg.KernelArgs)
	}
}

func TestSendToVM_FailsFastWhenSandboxNotRunning(t *testing.T) {
	rt := &FirecrackerRuntime{
		sandboxes: map[string]*managedSandbox{
			"sb-1": {
				info: SandboxInfo{
					Spec:  SandboxSpec{ID: "sb-1", Name: "sb-1", RootfsPath: "/rootfs.ext4", VsockCID: 7, Resources: Resources{VCPUs: 1, MemoryMB: 128}, NetworkPolicy: NetworkPolicy{DefaultDeny: true}},
					State: StateStopped,
				},
			},
		},
	}

	_, err := rt.SendToVM(context.Background(), "sb-1", map[string]string{"type": "status"})
	if err == nil {
		t.Fatal("expected error for stopped sandbox")
	}
	if !strings.Contains(err.Error(), "is not running") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExchangeJSONWithReader_AllowsFragmentedVsockHandshake(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		defer serverConn.Close()

		_, _ = serverConn.Write([]byte("OK"))
		time.Sleep(10 * time.Millisecond)
		_, _ = serverConn.Write([]byte(" 1073741827\n"))

		var req map[string]string
		_ = json.NewDecoder(serverConn).Decode(&req)
		_ = json.NewEncoder(serverConn).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"status": "ready"},
		})
	}()

	reader, err := readVsockConnectHandshake(clientConn)
	if err != nil {
		t.Fatalf("read handshake: %v", err)
	}

	rt := &FirecrackerRuntime{}
	raw, err := rt.exchangeJSONWithReader(clientConn, reader, "sb-1", map[string]string{"type": "status"})
	if err != nil {
		t.Fatalf("exchange json: %v", err)
	}

	if !strings.Contains(string(raw), "ready") {
		t.Fatalf("unexpected response: %s", string(raw))
	}
}
