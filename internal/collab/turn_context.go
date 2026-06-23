package collab

import (
	"context"
	"time"

	"AegisClaw/internal/transport/hubclient"
)

// FetchRelevantSince loads anchor context from Store for a channel turn.
func FetchRelevantSince(ctx context.Context, hub hubclient.Client, chID string, sinceSeq int, anchorSeqs []int) (anchors []map[string]interface{}, err error) {
	anchorRaw := make([]interface{}, len(anchorSeqs))
	for i, s := range anchorSeqs {
		anchorRaw[i] = s
	}
	resp, err := hub.Send(ctx, hubclient.Message{
		Source:      hub.AssignedID(),
		Destination: "store",
		Command:     "channel.get_relevant_since",
		Payload: map[string]interface{}{
			"channel_id":  chID,
			"since_seq":   sinceSeq,
			"anchor_seqs": anchorRaw,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	payload, _ := resp.Payload.(map[string]interface{})
	if raw, ok := payload["anchors"].([]interface{}); ok {
		for _, item := range raw {
			if m, ok := item.(map[string]interface{}); ok {
				anchors = append(anchors, m)
			}
		}
	}
	return anchors, nil
}