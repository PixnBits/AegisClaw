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

// bridgeSession is one signed JSON RPC stream to AegisHub or the host portal bridge.
type bridgeSession struct {
	conn    net.Conn
	encoder *json.Encoder
	decoder *json.Decoder
	priv    ed25519.PrivateKey
	viaHost bool
	mu      sync.Mutex
}

func newBridgeSession(conn net.Conn, priv ed25519.PrivateKey, viaHost bool) (*bridgeSession, error) {
	s := &bridgeSession{
		conn:    conn,
		encoder: json.NewEncoder(conn),
		decoder: json.NewDecoder(conn),
		priv:    priv,
		viaHost: viaHost,
	}
	if viaHost {
		return s, nil
	}
	if err := s.registerOnHub(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return s, nil
}

func (s *bridgeSession) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

func (s *bridgeSession) registerOnHub() error {
	pubStr := base64.StdEncoding.EncodeToString(s.priv.Public().(ed25519.PublicKey))
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
	signMessage(&reg, s.priv)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := encodeWithContext(ctx, s.conn, s.encoder, reg); err != nil {
		return fmt.Errorf("web-portal: failed to register with Hub: %w", err)
	}
	var resp map[string]interface{}
	if err := decodeWithContext(ctx, s.decoder, &resp); err != nil {
		return err
	}
	if errMsg, ok := resp["error"]; ok {
		return fmt.Errorf("hub rejected web-portal registration: %v", errMsg)
	}
	return nil
}

func (s *bridgeSession) call(ctx context.Context, action string, payload json.RawMessage) (*dashboard.APIResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn == nil {
		return nil, fmt.Errorf("bridge session closed")
	}

	dest := portalbridge.Destination(action)
	if s.viaHost {
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
	signMessage(&msg, s.priv)

	if err := encodeWithContext(ctx, s.conn, s.encoder, msg); err != nil {
		if err != context.DeadlineExceeded && err != context.Canceled {
			s.abortConn()
		}
		return nil, fmt.Errorf("failed to send %s via bridge: %w", action, err)
	}

	for {
		var raw json.RawMessage
		if err := decodeWithContext(ctx, s.decoder, &raw); err != nil {
			if err != context.DeadlineExceeded && err != context.Canceled {
				s.abortConn()
			}
			return nil, fmt.Errorf("failed to receive response for %s: %w", action, err)
		}
		var resp Message
		_ = json.Unmarshal(raw, &resp)
		if !s.viaHost && resp.Command == "ack" {
			continue
		}

		apiResp := &dashboard.APIResponse{}
		if resp.Command == "error" || resp.Command == "" {
			apiResp.Success = false
			apiResp.Error = bridgeResponseError(raw, &resp)
		} else {
			apiResp.Success = true
			if data, err := json.Marshal(resp.Payload); err == nil {
				apiResp.Data = data
			}
		}
		return apiResp, nil
	}
}

func (s *bridgeSession) abortConn() {
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

func encodeWithContext(ctx context.Context, conn net.Conn, enc *json.Encoder, v interface{}) error {
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(dl)
		defer conn.SetWriteDeadline(time.Time{})
	}
	return enc.Encode(v)
}

func decodeWithContext(ctx context.Context, dec *json.Decoder, v interface{}) error {
	type decodeResult struct {
		err error
	}
	resCh := make(chan decodeResult, 1)
	go func() {
		resCh <- decodeResult{err: dec.Decode(v)}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-resCh:
		return r.err
	}
}

func bridgeResponseError(raw json.RawMessage, resp *Message) string {
	if resp != nil && resp.Payload != nil {
		if s, ok := resp.Payload.(string); ok && s != "" {
			return s
		}
		if m, ok := resp.Payload.(map[string]interface{}); ok {
			if e, ok := m["error"].(string); ok && e != "" {
				return e
			}
		}
		text := fmt.Sprintf("%v", resp.Payload)
		if text != "" && text != "<nil>" {
			return text
		}
	}
	var hubEnvelope map[string]string
	if json.Unmarshal(raw, &hubEnvelope) == nil {
		if e := hubEnvelope["error"]; e != "" {
			return e
		}
	}
	return "hub or bridge rejected request"
}

func bridgeIOError(err error) bool {
	if err == nil {
		return false
	}
	if err == context.DeadlineExceeded || err == context.Canceled {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "bridge session closed") ||
		strings.Contains(msg, "failed to send") ||
		strings.Contains(msg, "failed to receive") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection")
}

// dialOutboundHubOrPortalBridge dials hub/portal-bridge from the guest (outbound paths only).
// The inverted guest listener (:9101) is managed separately by resilientBridgeClient.
func dialOutboundHubOrPortalBridge() (conn net.Conn, viaHost bool, err error) {
	socket := expandPath(getHubSocket())
	if st, statErr := os.Stat(socket); statErr == nil && !st.IsDir() {
		if c, dialErr := net.Dial("unix", socket); dialErr == nil {
			return c, false, nil
		}
	}

	if runningInFirecrackerGuest() {
		if c, dialErr := dialVsockWithRetry(vsock.Host, hubclient.HubVsockPort, 8, 250*time.Millisecond); dialErr == nil {
			return c, false, nil
		}
		if c, dialErr := dialVsockWithRetry(vsock.Host, hubclient.PortalBridgeVsockPort, 12, 500*time.Millisecond); dialErr == nil {
			return c, true, nil
		}
		return nil, false, fmt.Errorf("web-portal guest: failed host vsock :%d/:%d",
			hubclient.HubVsockPort, hubclient.PortalBridgeVsockPort)
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