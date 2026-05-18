package aegishub

// ForwardTimersList forwards timer listing to AegisHub.
func (c *Client) ForwardTimersList(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return c.sendRequest("timers.list", data)
}

// ForwardSignalsList forwards signal listing to AegisHub.
func (c *Client) ForwardSignalsList(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return c.sendRequest("signals.list", data)
}
