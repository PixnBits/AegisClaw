package main

import (
	"context"
	"sync"
	"time"

	"AegisClaw/internal/config"
	"AegisClaw/internal/sandbox"
	"AegisClaw/internal/transport/hubclient"

	"github.com/sirupsen/logrus"
)

var guestPortalBridgeStarted sync.Map // vmID -> struct{}

// startGuestPortalBridge connects host → web-portal guest for the JSON portal bridge
// when guest→host vsock :1030 is unavailable (Firecracker inverted path, like hub :9101).
func startGuestPortalBridge(vmID string) {
	if vmID == "" || cfg == nil || cfg.SandboxType != config.Firecracker {
		return
	}
	if _, loaded := guestPortalBridgeStarted.LoadOrStore(vmID, struct{}{}); loaded {
		return
	}
	go func() {
		defer guestPortalBridgeStarted.Delete(vmID)
		runGuestPortalBridge(cfg.StateDir, vmID)
	}()
}

func runGuestPortalBridge(stateDir, vmID string) {
	udsPath := sandbox.FirecrackerVsockUDSPath(stateDir, vmID)
	port := uint32(hubclient.GuestPortalBridgePort)

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		guestConn, err := dialFirecrackerVsockWithRetry(ctx, udsPath, port, 160, 500*time.Millisecond)
		cancel()
		if err != nil {
			logrus.Debugf("guest portal bridge %s: guest listener not ready yet: %v", vmID, err)
			time.Sleep(2 * time.Second)
			continue
		}

		logrus.Infof("guest portal bridge connected: %s (hybrid vsock :%d) — handling portal JSON bridge", vmID, port)
		handlePortalBridgeConn(guestConn)
		_ = guestConn.Close()
		logrus.Warnf("guest portal bridge %s disconnected; reconnecting", vmID)
		time.Sleep(1 * time.Second)
	}
}

func portalBridgeReadyLog(vmID string) {
	logrus.Infof("portal bridge: host will dial web-portal guest on hybrid vsock :%d (vm=%s)", hubclient.GuestPortalBridgePort, vmID)
}
