package channelfacilitator

import (
	"context"

	"AegisClaw/internal/collab"
	"AegisClaw/internal/transport/hubclient"
)

// Receiver handles inbound hub messages for the facilitator component.
type Receiver struct {
	Facilitator *Facilitator
}

// ProcessMessage dispatches facilitator commands.
func (r *Receiver) ProcessMessage(ctx context.Context, msg hubclient.Message) bool {
	switch msg.Command {
	case CmdUpdated:
		payload, _ := msg.Payload.(map[string]interface{})
		if payload != nil {
			r.Facilitator.HandleUpdated(ctx, payload)
		}
		return true
	default:
		return false
	}
}

// NewReceiver builds a facilitator wired to hub.
func NewReceiver(hub Hub) *Receiver {
	return &Receiver{Facilitator: &Facilitator{hub: hub, actors: map[string]*ChannelActor{}}}
}

// TraceInbound logs channel.updated receipt when tracing is enabled.
func TraceInbound(from string, chID string) {
	collab.Tracef(ComponentID, "channel.updated.recv", "ch=%s from=%s", chID, from)
}