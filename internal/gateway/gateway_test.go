package gateway

import (
	"context"
	"testing"
	"time"
)

// stubChannel implements Channel for testing.
type stubChannel struct {
	id      string
	healthy bool
	sent    []Message
}

func (s *stubChannel) ID() string       { return s.id }
func (s *stubChannel) Healthy() bool    { return s.healthy }
func (s *stubChannel) Send(_ context.Context, msg Message) error {
	s.sent = append(s.sent, msg)
	return nil
}
func (s *stubChannel) Start(ctx context.Context, sink chan<- Message) error {
	<-ctx.Done()
	return nil
}

func TestGateway_RegisterAndChannels(t *testing.T) {
	gw := New(func(_ context.Context, _ Message) (string, error) { return "", nil })
	gw.Register(&stubChannel{id: "ch1"})
	gw.Register(&stubChannel{id: "ch2"})
	gw.Register(&stubChannel{id: "ch1"}) // replace

	ids := gw.Channels()
	if len(ids) != 2 {
		t.Errorf("expected 2 channels, got %d", len(ids))
	}
}

func TestGateway_DispatchCallsRouteAndSendsReply(t *testing.T) {
	stub := &stubChannel{id: "test", healthy: true}
	called := make(chan Message, 1)
	gw := New(func(_ context.Context, msg Message) (string, error) {
		called <- msg
		return "pong", nil
	})
	gw.Register(stub)

	msg := Message{
		ID:        "1",
		ChannelID: "test",
		SenderID:  "alice",
		Text:      "ping",
		ReceivedAt: time.Now(),
	}
	gw.dispatch(context.Background(), msg)

	select {
	case got := <-called:
		if got.Text != "ping" {
			t.Errorf("expected route to receive 'ping', got %q", got.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("route function not called")
	}

	if len(stub.sent) != 1 || stub.sent[0].Text != "pong" {
		t.Errorf("expected reply 'pong' sent to channel, got %v", stub.sent)
	}
}

func TestGateway_DispatchTruncatesOversizedMessage(t *testing.T) {
	stub := &stubChannel{id: "test", healthy: true}
	var received Message
	gw := New(func(_ context.Context, msg Message) (string, error) {
		received = msg
		return "", nil
	})
	gw.Register(stub)

	large := make([]byte, maxMessageBytes+100)
	for i := range large {
		large[i] = 'x'
	}

	msg := Message{
		ID:        "2",
		ChannelID: "test",
		Text:      string(large),
		ReceivedAt: time.Now(),
	}
	gw.dispatch(context.Background(), msg)

	if len(received.Text) != maxMessageBytes+len("\n...[truncated]") {
		t.Errorf("expected truncated message length, got %d", len(received.Text))
	}
}

func TestNewHTTPWebhookChannel_InvalidAddr(t *testing.T) {
	_, err := NewHTTPWebhookChannel(ChannelConfig{
		ID:   "test",
		Addr: "notavalidaddr",
	})
	if err == nil {
		t.Fatal("expected error for invalid addr")
	}
}

func TestNewHTTPWebhookChannel_EmptyAddr(t *testing.T) {
	_, err := NewHTTPWebhookChannel(ChannelConfig{
		ID:   "test",
		Addr: "",
	})
	if err == nil {
		t.Fatal("expected error for empty addr")
	}
}

func TestHTTPWebhookChannel_ID(t *testing.T) {
	ch, err := NewHTTPWebhookChannel(ChannelConfig{
		ID:   "myhook",
		Addr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ch.ID() != "myhook" {
		t.Errorf("expected ID 'myhook', got %q", ch.ID())
	}
}
