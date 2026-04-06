package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// HTTPWebhookChannel is a Channel adapter that listens for HTTP POST requests
// on a configurable address.  Clients send a JSON body of the form:
//
//	{"sender_id": "alice", "text": "hello", "metadata": {"k": "v"}}
//
// Optionally clients may include the header:
//
//	X-AegisClaw-Secret: <secret>
//
// When a non-empty secret is configured the header must match, otherwise the
// request is rejected with HTTP 401.
//
// Replies from the daemon are returned as the HTTP response body:
//
//	{"reply": "..."}
//
// This design keeps the webhook channel stateless — no persistent connection
// is required between the client and the gateway.
type HTTPWebhookChannel struct {
	cfg       ChannelConfig
	server    *http.Server
	ready     chan struct{}
	pending   map[string]chan string
	pendingMu chan struct{} // acts as a mutex token
}

// NewHTTPWebhookChannel creates a new HTTPWebhookChannel from cfg.
// cfg.Addr must be a valid host:port string.
func NewHTTPWebhookChannel(cfg ChannelConfig) (*HTTPWebhookChannel, error) {
	if cfg.Addr == "" {
		return nil, fmt.Errorf("webhook channel %q: addr is required", cfg.ID)
	}
	if _, _, err := net.SplitHostPort(cfg.Addr); err != nil {
		return nil, fmt.Errorf("webhook channel %q: invalid addr %q: %w", cfg.ID, cfg.Addr, err)
	}
	mu := make(chan struct{}, 1)
	mu <- struct{}{} // mutex token
	return &HTTPWebhookChannel{
		cfg:       cfg,
		ready:     make(chan struct{}),
		pending:   make(map[string]chan string),
		pendingMu: mu,
	}, nil
}

// ID returns the channel ID from configuration.
func (w *HTTPWebhookChannel) ID() string { return w.cfg.ID }

// Healthy returns true when the HTTP server is listening.
func (w *HTTPWebhookChannel) Healthy() bool {
	select {
	case <-w.ready:
		return w.server != nil
	default:
		return false
	}
}

// webhookRequest is the expected JSON body of an inbound webhook POST.
type webhookRequest struct {
	SenderID string            `json:"sender_id"`
	Text     string            `json:"text"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// webhookResponse is the JSON body returned to the HTTP client.
type webhookResponse struct {
	Reply string `json:"reply,omitempty"`
	Error string `json:"error,omitempty"`
}

// Start begins listening on cfg.Addr and forwards inbound messages to sink.
// Replies from the daemon are returned synchronously in the HTTP response.
func (w *HTTPWebhookChannel) Start(ctx context.Context, sink chan<- Message) error {
	// We use a response channel per request to bridge the HTTP handler and the
	// gateway route loop.  Each request creates a tiny channel, writes the
	// message to sink with the response channel embedded in Metadata, waits for
	// the reply, and returns it to the HTTP client.
	//
	// The gateway dispatch loop calls Channel.Send when a reply is ready, which
	// writes into the per-request channel.
	w.server = &http.Server{
		Addr:         w.cfg.Addr,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	w.server.Handler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Secret check.
		if w.cfg.Secret != "" {
			got := r.Header.Get("X-AegisClaw-Secret")
			if got != w.cfg.Secret {
				http.Error(rw, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, int64(maxMessageBytes)+1))
		if err != nil {
			http.Error(rw, "failed to read body", http.StatusBadRequest)
			return
		}
		if len(body) > maxMessageBytes {
			http.Error(rw, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}

		var req webhookRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(rw, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Text) == "" {
			http.Error(rw, "text is required", http.StatusBadRequest)
			return
		}

		msgID := uuid.New().String()
		replyCh := make(chan string, 1)

		// Register pending reply.
		<-w.pendingMu
		w.pending[msgID] = replyCh
		w.pendingMu <- struct{}{}

		meta := req.Metadata
		if meta == nil {
			meta = make(map[string]string)
		}
		meta["_reply_id"] = msgID

		msg := Message{
			ID:         msgID,
			ChannelID:  w.cfg.ID,
			SenderID:   req.SenderID,
			Text:       req.Text,
			ReceivedAt: time.Now().UTC(),
			Metadata:   meta,
		}

		select {
		case sink <- msg:
		case <-r.Context().Done():
			<-w.pendingMu
			delete(w.pending, msgID)
			w.pendingMu <- struct{}{}
			http.Error(rw, "request cancelled", http.StatusRequestTimeout)
			return
		}

		// Wait for the reply (up to 55s to stay inside HTTP timeout).
		var replyText string
		select {
		case replyText = <-replyCh:
		case <-time.After(55 * time.Second):
		case <-r.Context().Done():
		}

		<-w.pendingMu
		delete(w.pending, msgID)
		w.pendingMu <- struct{}{}

		rw.Header().Set("Content-Type", "application/json")
		json.NewEncoder(rw).Encode(webhookResponse{Reply: replyText}) //nolint:errcheck
	})

	ln, err := net.Listen("tcp", w.cfg.Addr)
	if err != nil {
		return fmt.Errorf("webhook channel %q: listen %s: %w", w.cfg.ID, w.cfg.Addr, err)
	}
	close(w.ready)

	errCh := make(chan error, 1)
	go func() {
		if err := w.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		} else {
			errCh <- nil
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w.server.Shutdown(shutdownCtx) //nolint:errcheck
	return <-errCh
}

// Send delivers a reply to the pending HTTP request identified by
// msg.Metadata["_reply_id"].
func (w *HTTPWebhookChannel) Send(_ context.Context, msg Message) error {
	replyID := msg.Metadata["_reply_id"]
	if replyID == "" {
		return nil
	}
	<-w.pendingMu
	ch, ok := w.pending[replyID]
	w.pendingMu <- struct{}{}
	if !ok {
		return nil // request already timed out
	}
	select {
	case ch <- msg.Text:
	default:
	}
	return nil
}
