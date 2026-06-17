package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"AegisClaw/internal/dashboard"
	"AegisClaw/internal/portalbridge"
	"AegisClaw/internal/transport/hubclient"

	"github.com/mdlayher/vsock"
)

// hubBridgeClient implements dashboard.APIClient by speaking the project's
// standard signed Message protocol to the AegisHub (and through it to backends),
// or via the Host Daemon portal bridge (vsock 1030) when running inside a
// Firecracker guest without a shared hub unix socket.
type hubBridgeClient struct {
	conn    net.Conn
	encoder *json.Encoder
	decoder *json.Decoder
	priv    ed25519.PrivateKey
	mu      sync.Mutex
	viaHost bool // true when connected to daemon portal bridge (not direct hub)
}

func newHubBridgeClient() (dashboard.APIClient, error) {
	pub, priv := portalBridgeKey()

	conn, viaHost, err := dialHubOrPortalBridge()
	if err != nil {
		return nil, err
	}

	c := &hubBridgeClient{
		conn:    conn,
		encoder: json.NewEncoder(conn),
		decoder: json.NewDecoder(conn),
		priv:    priv,
		viaHost: viaHost,
	}

	if viaHost {
		// Portal bridge does not require hub registration.
		return c, nil
	}

	pubStr := base64.StdEncoding.EncodeToString(pub)
	reg := Message{
		Source:      "web-portal",
		Destination: "hub",
		Command:     "register",
		Payload: map[string]string{
			"public_key": pubStr,
			"version":    getBuildVersion(),
		},
		Timestamp: time.Now().Format(time.RFC3339),
		Signature:   "dummy",
	}
	signMessage(&reg, priv)

	if err := c.encoder.Encode(reg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("web-portal: failed to register with Hub: %w", err)
	}

	var resp map[string]interface{}
	if err := c.decoder.Decode(&resp); err != nil {
		conn.Close()
		return nil, err
	}
	if errMsg, ok := resp["error"]; ok {
		conn.Close()
		return nil, fmt.Errorf("hub rejected web-portal registration: %v", errMsg)
	}

	return c, nil
}

// dialHubOrPortalBridge connects the portal guest to backends.
// In Firecracker, guest outbound vsock dial to the host (:9999 / :1030) is unreliable;
// use the inverted guest hub bridge (guest listens :9101, host dials in) per store/court VMs.
func dialHubOrPortalBridge() (conn net.Conn, viaHost bool, err error) {
	socket := expandPath(getHubSocket())
	if st, statErr := os.Stat(socket); statErr == nil && !st.IsDir() {
		if c, dialErr := net.Dial("unix", socket); dialErr == nil {
			return c, false, nil
		}
	}

	if runningInFirecrackerGuest() {
		if c, dialErr := acceptGuestHubBridgeConn(); dialErr == nil {
			return c, false, nil
		}
		if c, dialErr := dialVsockWithRetry(vsock.Host, hubclient.HubVsockPort, 8, 250*time.Millisecond); dialErr == nil {
			return c, false, nil
		}
		if c, dialErr := dialVsockWithRetry(vsock.Host, hubclient.PortalBridgeVsockPort, 12, 500*time.Millisecond); dialErr == nil {
			return c, true, nil
		}
		return nil, false, fmt.Errorf("web-portal guest: failed guest hub bridge :%d or host vsock :%d/:%d",
			hubclient.GuestHubBridgePort, hubclient.HubVsockPort, hubclient.PortalBridgeVsockPort)
	}

	if c, dialErr := net.Dial("unix", socket); dialErr == nil {
		return c, false, nil
	}
	if c, dialErr := dialVsockWithRetry(vsock.Host, hubclient.HubVsockPort, 4, 200*time.Millisecond); dialErr == nil {
		return c, false, nil
	}
	return nil, false, fmt.Errorf("web-portal: failed to connect to Hub (unix %s and vsock :%d)",
		socket, hubclient.HubVsockPort)
}

func acceptGuestHubBridgeConn() (net.Conn, error) {
	ln, err := vsock.Listen(hubclient.GuestHubBridgePort, nil)
	if err != nil {
		return nil, err
	}
	defer ln.Close()
	conn, err := ln.Accept()
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func runningInFirecrackerGuest() bool {
	if os.Getenv("AEGIS_WEB_PORTAL_LISTEN_ADDR") != "" {
		return true
	}
	if data, err := os.ReadFile("/proc/cmdline"); err == nil {
		return strings.Contains(string(data), "aegis.web_portal_listen_addr=")
	}
	return false
}

func dialVsockWithRetry(cid uint32, port uint32, attempts int, delay time.Duration) (net.Conn, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		conn, err := vsock.Dial(cid, port, nil)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		time.Sleep(delay)
	}
	return nil, lastErr
}

func (c *hubBridgeClient) Call(ctx context.Context, action string, payload json.RawMessage) (*dashboard.APIResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	dest := portalbridge.Destination(action)
	if c.viaHost {
		// Host portal bridge performs final routing (store/agent/daemon).
		dest = "daemon"
	}

	var rawPayload interface{}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &rawPayload)
	}

	msg := Message{
		Source:      "web-portal",
		Destination: dest,
		Command:     action,
		Payload:     rawPayload,
		Timestamp:   time.Now().Format(time.RFC3339),
		Signature:   "",
	}
	signMessage(&msg, c.priv)

	if err := c.encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("failed to send %s via bridge: %w", action, err)
	}

	var resp Message
	if err := c.decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to receive response for %s: %w", action, err)
	}

	apiResp := &dashboard.APIResponse{}
	if resp.Command == "error" || resp.Command == "" {
		apiResp.Success = false
		apiResp.Error = fmt.Sprintf("%v", resp.Payload)
	} else {
		apiResp.Success = true
		if data, err := json.Marshal(resp.Payload); err == nil {
			apiResp.Data = data
		}
	}
	return apiResp, nil
}

type Message struct {
	Source      string      `json:"source"`
	Destination string      `json:"destination"`
	Command     string      `json:"command"`
	Payload     interface{} `json:"payload"`
	Timestamp   string      `json:"timestamp"`
	Signature   string      `json:"signature"`
}

func signMessage(msg *Message, priv ed25519.PrivateKey) {
	msgCopy := *msg
	msgCopy.Signature = ""
	data, _ := json.Marshal(msgCopy)
	signature := ed25519.Sign(priv, data)
	msg.Signature = base64.StdEncoding.EncodeToString(signature)
}

func getBuildVersion() string {
	return "dev"
}

func expandPath(path string) string {
	if len(path) > 2 && path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}

func getHubSocket() string {
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		return env
	}
	return "~/.aegis/hub.sock"
}
