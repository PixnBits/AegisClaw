package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"AegisClaw/internal/config"
	"AegisClaw/internal/sandbox"
	"AegisClaw/internal/transport/hubclient"

	"github.com/sirupsen/logrus"
)

var guestHubBridgeStarted sync.Map // vmID -> struct{}

// startGuestHubBridgesForSession starts host→guest hub bridges for a paired runtime.
func startGuestHubBridgesForSession(sessionID string) {
	if sessionID == "" || cfg == nil || cfg.SandboxType != config.Firecracker {
		return
	}
	startGuestHubBridge("memory-" + sessionID)
	startGuestHubBridge("agent-" + sessionID)
}

func startGuestHubBridge(vmID string) {
	if vmID == "" || cfg == nil || cfg.SandboxType != config.Firecracker {
		return
	}
	if _, loaded := guestHubBridgeStarted.LoadOrStore(vmID, struct{}{}); loaded {
		return
	}
	go func() {
		defer guestHubBridgeStarted.Delete(vmID)
		runGuestHubBridge(cfg.StateDir, hubSocketPath(), vmID)
	}()
}

func reconcileGuestHubBridges() {
	if cfg == nil || orchestrator == nil || cfg.SandboxType != config.Firecracker {
		return
	}
	time.Sleep(5 * time.Second)
	vms, err := orchestrator.ListVMs(context.Background())
	if err != nil {
		return
	}
	for _, vm := range vms {
		switch {
		case vm.ID == "store" || vm.ID == "network-boundary":
			startGuestHubBridge(vm.ID)
		case strings.HasPrefix(vm.ID, "agent-") || strings.HasPrefix(vm.ID, "memory-"):
			startGuestHubBridge(vm.ID)
		}
	}
}

func hubSocketPath() string {
	path := expandPath("~/.aegis/hub.sock")
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		path = expandPath(env)
	}
	return path
}

func runGuestHubBridge(stateDir, hubSocket, vmID string) {
	udsPath := sandbox.FirecrackerVsockUDSPath(stateDir, vmID)
	port := uint32(hubclient.GuestHubBridgePort)

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		guestConn, err := dialFirecrackerVsockWithRetry(ctx, udsPath, port, 160, 500*time.Millisecond)
		cancel()
		if err != nil {
			logrus.Debugf("guest hub bridge %s: guest listener not ready yet: %v", vmID, err)
			time.Sleep(1500 * time.Millisecond)
			continue
		}

		hubConn, err := net.Dial("unix", hubSocket)
		if err != nil {
			logrus.Warnf("guest hub bridge %s: hub dial failed: %v", vmID, err)
			_ = guestConn.Close()
			time.Sleep(2 * time.Second)
			continue
		}

		logrus.Infof("guest hub bridge connected: %s (vsock :%d) <-> AegisHub", vmID, port)
		bridgeDone := make(chan struct{}, 2)
		go func() {
			_, _ = io.Copy(hubConn, guestConn)
			bridgeDone <- struct{}{}
		}()
		go func() {
			_, _ = io.Copy(guestConn, hubConn)
			bridgeDone <- struct{}{}
		}()
		<-bridgeDone

		_ = guestConn.Close()
		_ = hubConn.Close()
		logrus.Warnf("guest hub bridge %s disconnected; reconnecting", vmID)
		time.Sleep(1 * time.Second)
	}
}

func dialFirecrackerVsockWithRetry(ctx context.Context, udsPath string, port uint32, attempts int, delay time.Duration) (net.Conn, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		conn, err := dialFirecrackerVsock(ctx, udsPath, port)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("exhausted %d dial attempts", attempts)
	}
	return nil, lastErr
}
