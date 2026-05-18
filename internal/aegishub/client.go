package aegishub

import (
	"context"
	"fmt"
)

// Client is a small placeholder for the AegisHub transport client. The real
// vsock protocol is wired by the daemon integration layer.
type Client struct {
	addr string
}

func NewClient(addr string) (*Client, error) {
	return &Client{addr: addr}, nil
}

func (c *Client) sendRequest(_ string, _ interface{}) (interface{}, error) {
	if c == nil {
		return nil, fmt.Errorf("nil AegisHub client")
	}
	return map[string]interface{}{"ok": true}, nil
}

// Health performs a health check against AegisHub.
func (c *Client) Health(ctx context.Context) error {
	_ = ctx
	_, err := c.sendRequest("health.ping", nil)
	if err != nil {
		return fmt.Errorf("AegisHub health check failed: %w", err)
	}
	return nil
}
