package aegishub

// ForwardApprovalsList forwards approval listing requests to AegisHub.
func (c *Client) ForwardApprovalsList(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return c.sendRequest("approvals.list", data)
}

// ForwardApprovalsDecide forwards approval decision requests to AegisHub.
func (c *Client) ForwardApprovalsDecide(ctx context.Context, data json.RawMessage) (*api.Response, error) {
	return c.sendRequest("approvals.decide", data)
}
