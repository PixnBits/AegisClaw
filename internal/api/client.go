package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
)

// Client communicates with the AegisClaw daemon over a Unix socket.
type Client struct {
	socketPath string
	http       *http.Client
}

// NewClient creates a client that connects to the daemon socket.
func NewClient(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
		},
	}
}

// Call sends a request to the daemon and returns the response.
func (c *Client) Call(ctx context.Context, action string, data any) (*Response, error) {
	var rawData json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("marshal request data: %w", err)
		}
		rawData = b
	}

	body, err := json.Marshal(Request{Action: action, Data: rawData})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://aegisclaw/api", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon not reachable at %s — is `sudo aegisclaw start` running? %w", c.socketPath, err)
	}
	defer resp.Body.Close()

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// Ping checks whether the daemon is reachable.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Call(ctx, "ping", nil)
	return err
}
