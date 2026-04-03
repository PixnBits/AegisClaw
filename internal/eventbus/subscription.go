package eventbus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SignalSource identifies the origin of an incoming signal.
type SignalSource string

const (
	SourceEmail    SignalSource = "email"
	SourceCalendar SignalSource = "calendar"
	SourceFile     SignalSource = "file"
	SourceGit      SignalSource = "git"
	SourceWebhook  SignalSource = "webhook"
	SourceCustom   SignalSource = "custom"
	SourceTimer    SignalSource = "timer" // internal: fired by the timer daemon
)

// Subscription represents an agent's registration to receive signals from a source.
type Subscription struct {
	// SubscriptionID is a UUID assigned on creation.
	SubscriptionID string `json:"subscription_id"`
	// Source is the signal origin (email, calendar, git, etc.).
	Source SignalSource `json:"source"`
	// Filter is arbitrary JSON used to narrow which events are delivered.
	Filter json.RawMessage `json:"filter,omitempty"`
	// TaskID links the subscription to an async task.
	TaskID string `json:"task_id,omitempty"`
	// Owner is the identity that created the subscription.
	Owner string `json:"owner"`
	// CreatedAt is the creation timestamp.
	CreatedAt time.Time `json:"created_at"`
	// Active indicates whether the subscription is still collecting signals.
	Active bool `json:"active"`
	// ReceivedCount is how many signals have been delivered.
	ReceivedCount int `json:"received_count"`
}

// Signal represents a received event from an external source or timer.
type Signal struct {
	// SignalID is a UUID assigned on receipt.
	SignalID string `json:"signal_id"`
	// Source is the origin of the signal.
	Source SignalSource `json:"source"`
	// Type categorises the event (reply, event, change, webhook, timer).
	Type string `json:"type"`
	// Payload is sanitized JSON event data.
	Payload json.RawMessage `json:"payload,omitempty"`
	// TaskID links the signal to a task (from its Subscription or Timer).
	TaskID string `json:"task_id,omitempty"`
	// SubscriptionID is set when the signal was delivered to a subscription.
	SubscriptionID string `json:"subscription_id,omitempty"`
	// TimerID is set when the signal was triggered by a timer.
	TimerID string `json:"timer_id,omitempty"`
	// ReceivedAt is when the signal arrived.
	ReceivedAt time.Time `json:"received_at"`
	// Processed marks whether the wakeup dispatcher has handled this signal.
	Processed bool `json:"processed"`
}

const subscriptionsFileName = "subscriptions.json"
const signalsFileName = "signals.json"

// subscriptionStore is the persistent storage layer for subscriptions + signals.
type subscriptionStore struct {
	subPath    string
	signalPath string
	mu         sync.RWMutex
	subs       map[string]*Subscription
	signals    map[string]*Signal
}

func newSubscriptionStore(dir string) (*subscriptionStore, error) {
	ss := &subscriptionStore{
		subPath:    filepath.Join(dir, subscriptionsFileName),
		signalPath: filepath.Join(dir, signalsFileName),
		subs:       make(map[string]*Subscription),
		signals:    make(map[string]*Signal),
	}
	if err := ss.loadSubs(); err != nil {
		return nil, err
	}
	if err := ss.loadSignals(); err != nil {
		return nil, err
	}
	return ss, nil
}

func (ss *subscriptionStore) loadSubs() error {
	raw, err := os.ReadFile(ss.subPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read subscriptions: %w", err)
	}
	var items []*Subscription
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("parse subscriptions: %w", err)
	}
	for _, s := range items {
		ss.subs[s.SubscriptionID] = s
	}
	return nil
}

func (ss *subscriptionStore) loadSignals() error {
	raw, err := os.ReadFile(ss.signalPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read signals: %w", err)
	}
	var items []*Signal
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("parse signals: %w", err)
	}
	for _, s := range items {
		ss.signals[s.SignalID] = s
	}
	return nil
}

func (ss *subscriptionStore) saveSubs() error {
	items := make([]*Subscription, 0, len(ss.subs))
	for _, s := range ss.subs {
		items = append(items, s)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(ss.subPath, raw)
}

func (ss *subscriptionStore) saveSignals() error {
	items := make([]*Signal, 0, len(ss.signals))
	for _, s := range ss.signals {
		items = append(items, s)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ReceivedAt.Before(items[j].ReceivedAt)
	})
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(ss.signalPath, raw)
}

func (ss *subscriptionStore) addSub(sub *Subscription) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.subs[sub.SubscriptionID] = sub
	return ss.saveSubs()
}

func (ss *subscriptionStore) deactivateSub(id string) (bool, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	sub, ok := ss.subs[id]
	if !ok || !sub.Active {
		return false, nil
	}
	sub.Active = false
	return true, ss.saveSubs()
}

func (ss *subscriptionStore) getSub(id string) (*Subscription, bool) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	s, ok := ss.subs[id]
	if !ok {
		return nil, false
	}
	cp := *s
	return &cp, true
}

func (ss *subscriptionStore) listSubs(activeOnly bool) []*Subscription {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	var out []*Subscription
	for _, s := range ss.subs {
		if activeOnly && !s.Active {
			continue
		}
		cp := *s
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (ss *subscriptionStore) countActive() int {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	n := 0
	for _, s := range ss.subs {
		if s.Active {
			n++
		}
	}
	return n
}

func (ss *subscriptionStore) addSignal(sig *Signal) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	// Bump received count on matching subscription.
	if sig.SubscriptionID != "" {
		if sub, ok := ss.subs[sig.SubscriptionID]; ok {
			sub.ReceivedCount++
		}
	}
	ss.signals[sig.SignalID] = sig
	if err := ss.saveSubs(); err != nil {
		return err
	}
	return ss.saveSignals()
}

func (ss *subscriptionStore) listSignals(taskID string, limit int) []*Signal {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	var out []*Signal
	for _, s := range ss.signals {
		if taskID != "" && s.TaskID != taskID {
			continue
		}
		cp := *s
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ReceivedAt.After(out[j].ReceivedAt) // newest first
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// newSubID generates a new UUID for a subscription.
func newSubID() string { return uuid.New().String() }

// newSignalID generates a new UUID for a signal.
func newSignalID() string { return uuid.New().String() }
