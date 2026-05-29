package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"AegisClaw/internal/dashboard"
	"AegisClaw/internal/transport/hubclient"

	"github.com/mdlayher/vsock"
)

// hubBridgeClient implements dashboard.APIClient by speaking the project's
// standard signed Message protocol to the AegisHub (and through it to the
// Host Daemon and other components).
//
// This is the mechanism that enforces the "thin / presentation-only" rule
// for the Web Portal.
type hubBridgeClient struct {
	conn    net.Conn
	encoder *json.Encoder
	decoder *json.Decoder
	priv    ed25519.PrivateKey
	mu      sync.Mutex
}

func newHubBridgeClient() (dashboard.APIClient, error) {
	socket := expandPath(getHubSocket())
	conn, err := net.Dial("unix", socket)
	if err != nil {
		// VM path (Firecracker): no unix socket filesystem sharing. Fall back to the
		// standard vsock control plane that AegisHub already listens on (port 9999).
		// This is the missing piece that lets the web-portal binary inside a real
		// microVM reach the Hub/daemon for rich dashboard actions (chat, approvals,
		// Canvas SSE, etc.). The unix path is kept for host-child dev and fixture runs.
		// See web-portal-vm.md §Communication and hubclient.DialVsock.
		if vconn, verr := vsock.Dial(vsock.Host, hubclient.HubVsockPort, nil); verr == nil {
			conn = vconn
			err = nil
		} else {
			return nil, fmt.Errorf("web-portal: failed to connect to Hub (unix and vsock both failed): %w (vsock err: %v)", err, verr)
		}
	}

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pubStr := base64.StdEncoding.EncodeToString(pub)

	c := &hubBridgeClient{
		conn:    conn,
		encoder: json.NewEncoder(conn),
		decoder: json.NewDecoder(conn),
		priv:    priv,
	}

	// Register using the exact same pattern as builder, store, agent, etc.
	reg := Message{
		Source:      "web-portal",
		Destination: "hub",
		Command:     "register",
		Payload: map[string]string{
			"public_key": pubStr,
			"version":    getBuildVersion(),
		},
		Timestamp: time.Now().Format(time.RFC3339),
		Signature: "dummy", // Allowed under AEGIS_DEV_MODE during development
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

func (c *hubBridgeClient) Call(ctx context.Context, action string, payload json.RawMessage) (*dashboard.APIResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	msg := Message{
		Source:      "web-portal",
		Destination: "daemon", // The Host Daemon's portal bridge handles most dashboard actions
		Command:     action,
		Payload:     payload,
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

// --- local helpers (duplicated for now; can be shared later) ---

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
