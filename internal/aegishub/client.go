package aegishub

// Health performs a health check against AegisHub.
func (c *Client) Health(ctx context.Context) error {
	_, err := c.sendRequest("health.ping", nil)
	if err != nil {
		return fmt.Errorf("AegisHub health check failed: %w", err)
	}
	return nil
}
