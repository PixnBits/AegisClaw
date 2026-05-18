package aegishub

// ForwardWorkerList forwards worker listing requests.
func (c *Client) ForwardWorkerList(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return c.sendRequest("worker.list", data)
}

// ForwardWorkerStatus forwards worker status requests.
func (c *Client) ForwardWorkerStatus(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return c.sendRequest("worker.status", data)
}
