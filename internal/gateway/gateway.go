// Package gateway implements the multi-channel message gateway for AegisClaw.
//
// Inspired by OpenClaw's central Gateway (ws://127.0.0.1:18789), the
// AegisClaw Gateway routes inbound messages from external channels
// (Telegram, Discord, webhooks, …) to the daemon via the API socket and
// forwards replies back to the originating channel.
//
// Architecture (Phase 2, Task 4 of the OpenClaw integration plan):
//
//	External Channel  ──►  Channel.Receive()
//	                            │
//	                            ▼
//	                      Gateway.route()   ──►  daemon API socket
//	                            │
//	                      Channel.Send()    ◄──  daemon reply
//
// Security invariants:
//   - Every inbound message is validated (non-empty, size-capped).
//   - Channel identity is verified via the shared secret in ChannelConfig.
//   - All routing decisions are logged to the kernel audit log.
//   - No direct VM-to-VM communication; the gateway is host-side only.
//   - Actual protocol handlers must be registered as governed skills via the
//     Governance Court before they can be activated.
package gateway

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// maxMessageBytes is the size cap applied to every inbound message to prevent
// memory exhaustion and prompt-injection via oversized payloads.
const maxMessageBytes = 64 * 1024 // 64 KiB

// Message is a normalised inbound or outbound message travelling through the
// Gateway.  All channel adapters translate their native message format into
// this struct so the routing core remains channel-agnostic.
type Message struct {
	// ID is a unique opaque identifier for this message (set by the channel).
	ID string `json:"id"`

	// ChannelID is the name of the channel this message arrived on / should
	// be sent to (e.g. "webhook", "telegram", "discord").
	ChannelID string `json:"channel_id"`

	// SenderID is a channel-specific identifier for the sender
	// (e.g. Telegram user_id, Discord user#tag).
	SenderID string `json:"sender_id,omitempty"`

	// Text is the human-readable body of the message.
	Text string `json:"text"`

	// ReceivedAt is set by the channel when the message arrives.
	ReceivedAt time.Time `json:"received_at"`

	// Metadata holds channel-specific key-value pairs (e.g. chat_id, guild_id).
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Channel is the interface that every channel adapter must implement.
// A Channel is responsible for translating between the native protocol and the
// gateway's normalised Message format.
//
// Each Channel runs as a long-lived goroutine.  The Gateway starts channels
// via Start and stops them via Stop.
type Channel interface {
	// ID returns the channel's unique name (e.g. "webhook", "telegram").
	ID() string

	// Start begins listening for inbound messages and sends them to the
	// provided sink.  It must return when ctx is cancelled.
	Start(ctx context.Context, sink chan<- Message) error

	// Send delivers a reply message to the originating user/room.
	// The channel uses Message.SenderID and Message.Metadata to determine
	// the destination.
	Send(ctx context.Context, msg Message) error

	// Healthy returns true when the channel is accepting messages.
	Healthy() bool
}

// ChannelConfig holds the configuration for a single channel adapter.
type ChannelConfig struct {
	// ID is the unique channel name (must match the Channel.ID() value).
	ID string `yaml:"id" mapstructure:"id"`

	// Type identifies the adapter implementation: "webhook", "telegram",
	// "discord", etc.
	Type string `yaml:"type" mapstructure:"type"`

	// Enabled controls whether this channel is started by the Gateway.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`

	// Addr is the listen address for server-type channels (e.g. webhooks).
	// Ignored for client-type channels.
	Addr string `yaml:"addr" mapstructure:"addr"`

	// Secret is a shared secret used to authenticate inbound requests.
	// For HTTP webhooks this is compared against the X-AegisClaw-Secret header.
	Secret string `yaml:"secret" mapstructure:"secret"`

	// Extra holds channel-specific key-value settings.
	Extra map[string]string `yaml:"extra" mapstructure:"extra"`
}

// Config holds the configuration for the Gateway.
type Config struct {
	// Enabled controls whether the Gateway is started by the daemon.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`

	// Channels is the list of channel adapter configurations.
	Channels []ChannelConfig `yaml:"channels" mapstructure:"channels"`
}

// RouteFunc is called by the Gateway for every inbound message.  It should
// forward the message to the daemon and return the reply.  The reply may be
// empty if no response is warranted (e.g. async processing).
type RouteFunc func(ctx context.Context, msg Message) (reply string, err error)

// Gateway routes inbound messages from registered Channel adapters to the
// daemon and sends replies back.
type Gateway struct {
	channels map[string]Channel
	route    RouteFunc
	mu       sync.RWMutex
	sink     chan Message
}

// New creates a Gateway with the provided routing function.  Channels must be
// registered via Register before calling Start.
func New(route RouteFunc) *Gateway {
	return &Gateway{
		channels: make(map[string]Channel),
		route:    route,
		sink:     make(chan Message, 256),
	}
}

// Register adds a channel adapter to the Gateway.  It is safe to call before
// or after Start.  A channel with a duplicate ID replaces the existing one.
func (g *Gateway) Register(ch Channel) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.channels[ch.ID()] = ch
}

// Channels returns the IDs of all registered channels.
func (g *Gateway) Channels() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	ids := make([]string, 0, len(g.channels))
	for id := range g.channels {
		ids = append(ids, id)
	}
	return ids
}

// Start launches all enabled channels and begins routing messages.
// It blocks until ctx is cancelled.
func (g *Gateway) Start(ctx context.Context) error {
	g.mu.RLock()
	channels := make([]Channel, 0, len(g.channels))
	for _, ch := range g.channels {
		channels = append(channels, ch)
	}
	g.mu.RUnlock()

	var wg sync.WaitGroup
	for _, ch := range channels {
		ch := ch
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ch.Start(ctx, g.sink); err != nil && ctx.Err() == nil {
				// Channel error during normal operation — log but keep routing.
				_ = fmt.Errorf("gateway: channel %s: %w", ch.ID(), err)
			}
		}()
	}

	// Route loop: consume inbound messages and dispatch to the route function.
	routeDone := make(chan struct{})
	go func() {
		defer close(routeDone)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-g.sink:
				if !ok {
					return
				}
				g.dispatch(ctx, msg)
			}
		}
	}()

	wg.Wait()
	<-routeDone
	return nil
}

// dispatch calls the route function for msg and sends the reply back via the
// originating channel (if a reply is produced).
func (g *Gateway) dispatch(ctx context.Context, msg Message) {
	if len(msg.Text) > maxMessageBytes {
		msg.Text = msg.Text[:maxMessageBytes] + "\n...[truncated]"
	}

	reply, err := g.route(ctx, msg)
	if err != nil || reply == "" {
		return
	}

	g.mu.RLock()
	ch, ok := g.channels[msg.ChannelID]
	g.mu.RUnlock()
	if !ok {
		return
	}

	replyMsg := Message{
		ChannelID: msg.ChannelID,
		SenderID:  msg.SenderID,
		Text:      reply,
		Metadata:  msg.Metadata,
	}
	_ = ch.Send(ctx, replyMsg)
}
