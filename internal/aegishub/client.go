package aegishub

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/mdlayher/vsock"
)

// Client is a real vsock-based client to AegisHub.
type Client struct {
	conn net.Conn
}

// NewClient creates a vsock client to AegisHub.
// addr should be in the form "vsock://<cid>:<port>" or we use defaults.
func NewClient(addr string) (*Client, error) {
	// For now use fixed CID/port for AegisHub (similar to Store VM pattern)
	// In production this would come from config.
	const (
		aegisHubCID  = 2
		aegisHubPort = 9998
	)

	conn, err := vsock.Dial(aegisHubCID, aegisHubPort)
	if err != nil {
		return nil, fmt.Errorf("vsock dial to AegisHub failed: %w", err)
	}
	return &Client{conn: conn}, nil
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// ForwardChatMessage sends a chat message request to AegisHub.
func (c *Client) ForwardChatMessage(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return c.sendRequest("chat.message", data)
}

// ForwardChatTool sends a tool execution request to AegisHub.
func (c *Client) ForwardChatTool(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return c.sendRequest("chat.tool", data)
}

func (c *Client) sendRequest(op string, payload json.RawMessage) (*api.Response, error) {
	req := map[string]interface{}{
		"op":      op,
		"payload": payload,
	}

	encoder := json.NewEncoder(c.conn)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("encode request to AegisHub: %w", err)
	}

	decoder := json.NewDecoder(c.conn)
	var resp api.Response
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response from AegisHub: %w", err)
	}

	return &resp, nil
}
