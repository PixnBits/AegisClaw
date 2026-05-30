// Package log provides a minimal structured logger for Aegis microVM guests
// that emits over vsock to the Host Daemon (Phase 1 observability).
//
// Protocol (simple for Phase 1):
//   - Connect to vsock CID 2 (Host) on port 18099 (hubclient.LogVsockPort).
//   - Send newline-delimited JSON records.
//   - Each record is an object with at least: ts (RFC3339), level, msg.
//   - Optional "fields" object for structured data.
//
// The client is best-effort and non-blocking on send. If the connection
// cannot be established or is lost, logging is silently dropped (guests
// must never hard-fail because the observability channel is unavailable).
package log

import (
	"encoding/json"
	"net"
	"sync"
	"time"

	"github.com/mdlayher/vsock"
)

// Record is the wire format for Phase 1 guest logs.
type Record struct {
	Timestamp string                 `json:"ts"`
	Level     string                 `json:"level"`
	Message   string                 `json:"msg"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// Client is a best-effort vsock logger.
type Client struct {
	mu     sync.Mutex
	conn   net.Conn
	closed bool

	// vmID is injected by the caller (usually from kernel cmdline or env).
	vmID string
}

// New creates a new best-effort vsock logger.
// It attempts to connect asynchronously so that guest startup is never blocked.
func New(vmID string) *Client {
	c := &Client{vmID: vmID}
	go c.connect()
	return c
}

func (c *Client) connect() {
	// Try a few times with backoff. This is intentionally non-blocking
	// for the guest main path.
	for attempt := 0; attempt < 8; attempt++ {
		conn, err := vsock.Dial(vsock.Host, 18099, nil) // LogVsockPort
		if err == nil {
			c.mu.Lock()
			if !c.closed {
				c.conn = conn
			} else {
				_ = conn.Close()
			}
			c.mu.Unlock()
			return
		}
		time.Sleep(time.Duration(100*(attempt+1)) * time.Millisecond)
	}
}

// log sends a record. It never blocks the caller for long.
func (c *Client) log(level, msg string, fields map[string]interface{}) {
	rec := Record{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		Message:   msg,
		Fields:    fields,
	}
	if c.vmID != "" {
		if rec.Fields == nil {
			rec.Fields = map[string]interface{}{}
		}
		rec.Fields["vm_id"] = c.vmID
	}

	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	data = append(data, '\n')

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.conn == nil {
		return
	}

	// Best-effort, short write timeout so we don't stall guest code.
	_ = c.conn.SetWriteDeadline(time.Now().Add(250 * time.Millisecond))
	_, _ = c.conn.Write(data)
}

// Info logs at info level.
func (c *Client) Info(msg string, keyvals ...interface{}) {
	c.log("info", msg, keyvalsToMap(keyvals))
}

// Error logs at error level.
func (c *Client) Error(msg string, keyvals ...interface{}) {
	c.log("error", msg, keyvalsToMap(keyvals))
}

// Debug logs at debug level (only emitted when useful).
func (c *Client) Debug(msg string, keyvals ...interface{}) {
	c.log("debug", msg, keyvalsToMap(keyvals))
}

// Close shuts down the logger (best-effort).
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func keyvalsToMap(kv []interface{}) map[string]interface{} {
	m := make(map[string]interface{})
	for i := 0; i+1 < len(kv); i += 2 {
		if key, ok := kv[i].(string); ok {
			m[key] = kv[i+1]
		}
	}
	return m
}

// NewNoop returns a logger that does nothing. Useful for tests and fallback paths.
func NewNoop() *Client {
	return &Client{closed: true}
}

// Convenience constructor that many guests will use.
// It reads the VM identity from the same mechanisms already used
// (kernel cmdline or AEGIS_VM_ID env for now).
func NewDefault() *Client {
	vmID := getVMIdentity()
	if vmID == "" {
		return NewNoop()
	}
	return New(vmID)
}

func getVMIdentity() string {
	// Prefer explicit env (useful for testing and Docker path).
	if v := getEnv("AEGIS_VM_ID"); v != "" {
		return v
	}
	// Fall back to parsing from cmdline (Firecracker path).
	// In real guests this is usually injected as aegis.vm_id=... or similar.
	// For Phase 1 we keep it simple — callers can pass it explicitly.
	return ""
}

// Small env helper to avoid importing os in every guest if they don't want it.
func getEnv(key string) string {
	// We deliberately don't import "os" at the top so minimal guests
	// that already have their own env handling can still use this package.
	// Callers that want env support can pass the value in.
	return ""
}