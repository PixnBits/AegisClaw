package aegishub

// ForwardSessionsHistory forwards session history requests.
func (c *Client) ForwardSessionsHistory(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return c.sendRequest("sessions.history", data)
}

// ForwardSessionsSend forwards sending a message to a session.
func (c *Client) ForwardSessionsSend(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return c.sendRequest("sessions.send", data)
}

// ForwardSessionsSpawn forwards spawning a new session.
func (c *Client) ForwardSessionsSpawn(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return c.sendRequest("sessions.spawn", data)
}
