package channelfacilitator

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"sync/atomic"
	"time"

	"AegisClaw/internal/transport/hubclient"
)

var ephemeralSeq uint64

// EphemeralHub sends hub RPCs on short-lived connections (avoids decoder races with Receive loop).
type EphemeralHub struct {
	socket string
}

// NewEphemeralHub dials the host hub socket for outbound facilitator RPCs.
func NewEphemeralHub() *EphemeralHub {
	socket := os.ExpandEnv("$HOME/.aegis/hub.sock")
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		socket = env
	}
	return &EphemeralHub{socket: socket}
}

// Fire sends a signed one-way hub message (no RPC response wait).
func (e *EphemeralHub) Fire(ctx context.Context, msg hubclient.Message) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	client, err := hubclient.DialUnix(e.socket, priv)
	if err != nil {
		return err
	}
	defer client.Close()
	n := atomic.AddUint64(&ephemeralSeq, 1)
	id := ComponentID + "-out-" + formatUint(n)
	if _, err := client.Register(ctx, id, pub, "phase1"); err != nil {
		return err
	}
	msg.Source = id
	return client.Reply(ctx, msg)
}

func (e *EphemeralHub) Send(ctx context.Context, msg hubclient.Message) (hubclient.Message, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return hubclient.Message{}, err
	}
	client, err := hubclient.DialUnix(e.socket, priv)
	if err != nil {
		return hubclient.Message{}, err
	}
	defer client.Close()
	n := atomic.AddUint64(&ephemeralSeq, 1)
	id := ComponentID + "-out-" + formatUint(n)
	if _, err := client.Register(ctx, id, pub, "phase1"); err != nil {
		return hubclient.Message{}, err
	}
	msg.Source = id
	return client.Send(ctx, msg)
}

func formatUint(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// OutboundSender is used by Facilitator for store/orchestrator/turn delivery.
type OutboundSender interface {
	Send(ctx context.Context, msg hubclient.Message) (hubclient.Message, error)
}

// noop timeout helper
func rpcTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, 30*time.Second)
}