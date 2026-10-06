package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"AegisClaw/internal/config"
	"AegisClaw/internal/runtime"
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
	// Short initial delay only (was 5s). Individual bridge dial loops already use
	// 100ms/200ms retries with long timeouts, and session bridges are started early
	// via startGuestHubBridgesForSession. This keeps reconcile from adding unnecessary
	// wall time before "ready for use" feel after sudo ./bin/aegis start.
	time.Sleep(200 * time.Millisecond)
	reconcileGuestHubBridgesOnce()
	// Court VMs start lazily after store readiness (after this one-shot reconcile).
	// Keep reconciling so late-launched court-persona-* / court-scribe get bridges.
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			reconcileGuestHubBridgesOnce()
		}
	}()
}

func reconcileGuestHubBridgesOnce() {
	if cfg == nil || orchestrator == nil || cfg.SandboxType != config.Firecracker {
		return
	}
	vms, err := orchestrator.ListVMs(context.Background())
	if err != nil {
		return
	}
	for _, vm := range vms {
		switch {
		case vm.ID == "store" || vm.ID == "network-boundary" || vm.ID == "web-portal":
			startGuestHubBridge(vm.ID)
		case strings.HasPrefix(vm.ID, "agent-") || strings.HasPrefix(vm.ID, "memory-"):
			startGuestHubBridge(vm.ID)
		case vm.ID == "project-manager" || strings.HasPrefix(vm.ID, "project-manager-"):
			startGuestHubBridge(vm.ID)
		case strings.HasPrefix(vm.ID, "coder-") || strings.HasPrefix(vm.ID, "tester-"):
			startGuestHubBridge(vm.ID)
		case vm.ID == "court-scribe" || strings.HasPrefix(vm.ID, "court-persona-"):
			startGuestHubBridge(vm.ID)
		}
	}
}

// startCourtGuestHubBridges starts hub bridges for Court VMs launched after the initial reconcile.
func startCourtGuestHubBridges() {
	startGuestHubBridge("court-scribe")
	for _, p := range []string{
		"ciso", "security-architect", "architect", "senior-coder",
		"tester", "efficiency", "user-advocate",
	} {
		startGuestHubBridge("court-persona-" + p)
	}
}

func hubSocketPath() string {
	path := expandPath("~/.aegis/hub.sock")
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		path = expandPath(env)
	}
	return path
}

// guestBridgeRegisterReadTimeout bounds the guest's first line. Tests may
// shorten it; the daemon uses 60s.
var guestBridgeRegisterReadTimeout = 60 * time.Second

const guestBridgeMaxRegisterLine = 64 * 1024

var errGuestBridgeLineTooLong = errors.New("register line exceeds 64KiB")

// guestHubDialError is a hub dial failure. The run loop keeps the shorter
// retry used before the register check; register refusals back off longer.
type guestHubDialError struct {
	err error
}

func (e *guestHubDialError) Error() string {
	if e == nil || e.err == nil {
		return "hub dial failed"
	}
	return e.err.Error()
}

func (e *guestHubDialError) Unwrap() error { return e.err }

func runGuestHubBridge(stateDir, hubSocket, vmID string) {
	udsPath := sandbox.FirecrackerVsockUDSPath(stateDir, vmID)
	port := uint32(hubclient.GuestHubBridgePort)

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		guestConn, err := dialFirecrackerVsockWithRetry(ctx, udsPath, port, 120, 500*time.Millisecond)
		cancel()
		if err != nil {
			logrus.Debugf("guest hub bridge %s: guest listener not ready yet: %v", vmID, err)
			// Reduced sleep for faster readiness (was 1500ms); helps <1s agent guest hub_dialed
			// (the main remaining pole after other opts). Overlaps with guest boot via early start.
			time.Sleep(100 * time.Millisecond)
			continue
		}

		err = bridgeGuestConn(vmID, guestConn, func() (net.Conn, error) {
			return net.Dial("unix", hubSocket)
		})
		if err != nil {
			var dialErr *guestHubDialError
			if errors.As(err, &dialErr) {
				logrus.Warnf("guest hub bridge %s: hub dial failed: %v", vmID, dialErr.err)
				time.Sleep(200 * time.Millisecond)
				continue
			}
			// Refused register is already audited. Back off at least as long as a
			// disconnect so a hostile guest cannot spin the dial loop.
			time.Sleep(500 * time.Millisecond)
			continue
		}
		logrus.Warnf("guest hub bridge %s disconnected; reconnecting", vmID)
		time.Sleep(500 * time.Millisecond)
	}
}

// guestBridgeRegisterAllowed reports whether a guest on the bridge for vmID
// may register as source.
//
// store, court-scribe, and court-persona-* are allowed only when vmID is that
// exact id. The store VM and Court VMs register through their own bridges;
// blocking those ids outright would break Firecracker. Host-only ids are never
// accepted from a guest bridge, even when vmID equals source.
func guestBridgeRegisterAllowed(vmID, source string) (bool, string) {
	if source == "" {
		return false, "empty source"
	}
	if guestBridgeHostOnlyID(source) {
		return false, "host-only id"
	}
	if vmID == "" {
		return false, "empty vm id"
	}
	if source != vmID {
		return false, "source does not match vm id"
	}
	return true, ""
}

func guestBridgeHostOnlyID(id string) bool {
	// Same set as before, owned by runtime.HostOnlyVMID. Not ReservedVMIDReason:
	// that list is wider (store, court, …), case-insensitive, and its "hub"
	// dash-boundary would reject hub-perm-fetcher.
	return runtime.HostOnlyVMID(id)
}

// bridgeGuestConn checks the guest's first hub line, then pipes the connection.
// It owns guestConn and closes it on every return.
func bridgeGuestConn(vmID string, guestConn net.Conn, dialHub func() (net.Conn, error)) error {
	if guestConn == nil {
		return errors.New("guest hub bridge: nil guest conn")
	}
	if dialHub == nil {
		return refuseGuestBridgeRegister(vmID, "", "nil hub dialer", guestConn)
	}

	line, msg, br, err := readGuestBridgeRegister(guestConn)
	if err != nil {
		return refuseGuestBridgeRegister(vmID, "", err.Error(), guestConn)
	}
	if msg.Destination != "hub" || msg.Command != "register" {
		reason := fmt.Sprintf("first message destination %q command %q", msg.Destination, msg.Command)
		return refuseGuestBridgeRegister(vmID, msg.Source, reason, guestConn)
	}
	if ok, reason := guestBridgeRegisterAllowed(vmID, msg.Source); !ok {
		return refuseGuestBridgeRegister(vmID, msg.Source, reason, guestConn)
	}

	if err := guestConn.SetReadDeadline(time.Time{}); err != nil {
		_ = guestConn.Close()
		return fmt.Errorf("guest hub bridge %s: clear read deadline: %w", vmID, err)
	}

	hubConn, err := dialHub()
	if err != nil || hubConn == nil {
		_ = guestConn.Close()
		if err == nil {
			err = errors.New("nil hub conn")
		}
		return &guestHubDialError{err: err}
	}

	if _, err := io.Copy(hubConn, bytes.NewReader(line)); err != nil {
		_ = guestConn.Close()
		_ = hubConn.Close()
		return fmt.Errorf("guest hub bridge %s: write register: %w", vmID, err)
	}

	logrus.Infof("guest hub bridge connected: %s (vsock :%d) <-> AegisHub", vmID, hubclient.GuestHubBridgePort)

	bridgeDone := make(chan struct{}, 2)
	go func() {
		// Copy from the reader so bytes buffered past the first line are kept.
		_, _ = io.Copy(hubConn, br)
		bridgeDone <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(guestConn, hubConn)
		bridgeDone <- struct{}{}
	}()
	<-bridgeDone
	_ = guestConn.Close()
	_ = hubConn.Close()
	<-bridgeDone
	return nil
}

func refuseGuestBridgeRegister(vmID, source, reason string, guestConn net.Conn) error {
	logrus.Warnf("Audit: guest hub bridge %s refused register as %q: %s", vmID, source, reason)
	if guestConn != nil {
		_ = guestConn.Close()
	}
	return fmt.Errorf("guest hub bridge %s refused register as %q: %s", vmID, source, reason)
}

func readGuestBridgeRegister(guestConn net.Conn) (line []byte, msg hubclient.Message, br *bufio.Reader, err error) {
	if err = guestConn.SetReadDeadline(time.Now().Add(guestBridgeRegisterReadTimeout)); err != nil {
		return nil, hubclient.Message{}, nil, fmt.Errorf("set read deadline: %w", err)
	}
	br = bufio.NewReader(guestConn)
	line, err = readLimitedLine(br, guestBridgeMaxRegisterLine)
	if err != nil {
		return nil, hubclient.Message{}, nil, err
	}
	if err = json.Unmarshal(line, &msg); err != nil {
		return nil, hubclient.Message{}, nil, errors.New("invalid register json")
	}
	return line, msg, br, nil
}

// readLimitedLine returns one newline-terminated line of at most max bytes.
// It stops as soon as the cap is exceeded, so a guest that never sends a
// newline cannot pin the bridge waiting to fill a bufio buffer.
func readLimitedLine(r *bufio.Reader, max int) ([]byte, error) {
	out := make([]byte, 0, 512)
	for {
		if len(out) >= max {
			return nil, errGuestBridgeLineTooLong
		}
		b, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read register: %w", err)
		}
		out = append(out, b)
		if b == '\n' {
			return out, nil
		}
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
