package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/dashboard"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"go.uber.org/zap"
)

const (
	portalRootfsEnvKey   = "AEGISCLAW_PORTAL_ROOTFS"
	portalGuestVsockPort = 18080
	portalAPIVsockPort   = 1030
	maxPortalPayload     = 2 * 1024 * 1024 // 2 MiB
	portalDefaultTimeout = 30 * time.Second
	portalChatTimeout    = 2 * time.Minute
)

type portalBridgeRequest struct {
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// startDashboard starts the dashboard as a dedicated portal microVM and exposes
// a localhost edge proxy for browser access.
func startDashboard(ctx context.Context, env *runtimeEnv, apiSrv *api.Server) {
	if env.Config == nil || !env.Config.Dashboard.Enabled {
		return
	}

	addr := env.Config.Dashboard.Addr
	if addr == "" {
		addr = "127.0.0.1:7878"
	}
	portalVMID, err := ensurePortalVM(ctx, env)
	if err != nil {
		env.Logger.Error("dashboard portal vm start failed", zap.Error(err))
		return
	}
	if err := startPortalAPIBridge(ctx, env, apiSrv, portalVMID); err != nil {
		env.Logger.Error("dashboard portal API bridge failed", zap.Error(err))
		return
	}
	if err := startPortalEdgeProxy(ctx, env, portalVMID, addr); err != nil {
		env.Logger.Error("dashboard edge proxy failed", zap.Error(err))
		return
	}
	env.Logger.Info("dashboard portal ready", zap.String("addr", addr), zap.String("vm_id", portalVMID))
}

func ensurePortalVM(ctx context.Context, env *runtimeEnv) (string, error) {
	env.portalVMMu.Lock()
	defer env.portalVMMu.Unlock()

	if env.PortalVMID != "" {
		sandboxes, err := env.Runtime.List(ctx)
		if err == nil {
			for _, sb := range sandboxes {
				if sb.Spec.ID == env.PortalVMID && sb.State == sandbox.StateRunning {
					return env.PortalVMID, nil
				}
			}
		}
		env.Logger.Warn("portal VM missing, recreating", zap.String("vm_id", env.PortalVMID))
		env.PortalVMID = ""
	}

	portalRootfs := os.Getenv(portalRootfsEnvKey)
	if portalRootfs == "" {
		portalRootfs = filepath.Join(filepath.Dir(env.Config.Rootfs.Template), "portal-rootfs.ext4")
	}
	if _, err := os.Stat(portalRootfs); err != nil {
		return "", fmt.Errorf("portal rootfs not found at %s (set %s to override): %w", portalRootfs, portalRootfsEnvKey, err)
	}

	portalID := generateVMID("portal")
	spec := sandbox.SandboxSpec{
		ID:   portalID,
		Name: "aegisclaw-portal",
		Resources: sandbox.Resources{
			VCPUs:    1,
			MemoryMB: 256,
		},
		NetworkPolicy: sandbox.NetworkPolicy{
			NoNetwork:   true,
			DefaultDeny: true,
		},
		RootfsPath: portalRootfs,
		InitPath:   "/sbin/aegisportal",
	}

	if err := env.Runtime.Create(ctx, spec); err != nil {
		return "", fmt.Errorf("create portal VM: %w", err)
	}
	if err := env.Runtime.Start(ctx, portalID); err != nil {
		env.Runtime.Delete(ctx, portalID) //nolint:errcheck
		return "", fmt.Errorf("start portal VM: %w", err)
	}

	env.PortalVMID = portalID
	return portalID, nil
}

func startPortalAPIBridge(ctx context.Context, env *runtimeEnv, apiSrv *api.Server, vmID string) error {
	listenPath, err := env.Runtime.VsockCallbackPath(vmID, portalAPIVsockPort)
	if err != nil {
		return fmt.Errorf("resolve portal API callback path: %w", err)
	}
	_ = os.Remove(listenPath)
	ln, err := net.Listen("unix", listenPath)
	if err != nil {
		return fmt.Errorf("listen portal API callback %s: %w", listenPath, err)
	}
	_ = os.Chmod(listenPath, 0666)

	go func() {
		<-ctx.Done()
		ln.Close() //nolint:errcheck
	}()

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go handlePortalAPIBridgeConn(env, apiSrv, conn)
		}
	}()

	env.Logger.Info("dashboard portal API bridge listening", zap.String("socket", listenPath), zap.Uint("port", portalAPIVsockPort))
	return nil
}

func handlePortalAPIBridgeConn(env *runtimeEnv, apiSrv *api.Server, conn net.Conn) {
	defer conn.Close()

	dec := json.NewDecoder(io.LimitReader(conn, maxPortalPayload))
	enc := json.NewEncoder(conn)

	var req portalBridgeRequest
	if err := dec.Decode(&req); err != nil {
		_ = enc.Encode(&dashboard.APIResponse{Error: "decode: " + err.Error()})
		return
	}
	deadline := portalDefaultTimeout
	if req.Action == "chat.message" || req.Action == "chat.summarize" {
		deadline = portalChatTimeout
	}
	_ = conn.SetDeadline(time.Now().Add(deadline))
	if strings.TrimSpace(req.Action) == "" {
		_ = enc.Encode(&dashboard.APIResponse{Error: "action required"})
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if env != nil && env.Logger != nil {
				env.Logger.Error("dashboard portal API bridge panic",
					zap.String("action", req.Action),
					zap.Any("panic", recovered),
					zap.ByteString("stack", debug.Stack()),
				)
			}
			_ = enc.Encode(&dashboard.APIResponse{Error: "internal bridge panic"})
		}
	}()

	resp := apiSrv.CallDirect(context.Background(), req.Action, req.Payload)
	if resp == nil {
		_ = enc.Encode(&dashboard.APIResponse{Error: "unknown action: " + req.Action})
		return
	}
	if err := enc.Encode(&dashboard.APIResponse{
		Success: resp.Success,
		Error:   resp.Error,
		Data:    resp.Data,
	}); err != nil && env != nil && env.Logger != nil {
		env.Logger.Warn("dashboard portal API bridge encode failed",
			zap.String("action", req.Action),
			zap.Error(err),
		)
	}
}

func startPortalEdgeProxy(ctx context.Context, env *runtimeEnv, vmID, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen dashboard edge proxy on %s: %w", addr, err)
	}

	go func() {
		<-ctx.Done()
		ln.Close() //nolint:errcheck
	}()

	go func() {
		for {
			clientConn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go proxyToPortalVM(ctx, env, vmID, clientConn)
		}
	}()
	return nil
}

func proxyToPortalVM(ctx context.Context, env *runtimeEnv, vmID string, clientConn net.Conn) {
	defer clientConn.Close()

	upstreamConn, err := dialVMVsockGuestPort(ctx, env, vmID, portalGuestVsockPort)
	if err != nil {
		env.Logger.Warn("portal edge proxy dial failed", zap.Error(err))
		return
	}
	defer upstreamConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(upstreamConn, clientConn)
		_ = upstreamConn.SetDeadline(time.Now())
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(clientConn, upstreamConn)
		_ = clientConn.SetDeadline(time.Now())
	}()
	wg.Wait()
}

func dialVMVsockGuestPort(ctx context.Context, env *runtimeEnv, vmID string, port int) (net.Conn, error) {
	vsockPath, err := env.Runtime.VsockPath(vmID)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", vsockPath)
	if err != nil {
		return nil, fmt.Errorf("dial vsock path %s: %w", vsockPath, err)
	}

	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", port); err != nil {
		conn.Close()
		return nil, fmt.Errorf("vsock connect handshake write: %w", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("vsock connect handshake read: %w", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	if !strings.HasPrefix(line, "OK") {
		conn.Close()
		return nil, fmt.Errorf("vsock connect rejected: %s", strings.TrimSpace(line))
	}
	return conn, nil
}
