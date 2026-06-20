package testutil

import (
	"AegisClaw/internal/portalstomp"
)

// FakeSTOMPSession drives subscription lifecycle in contract tests.
type FakeSTOMPSession struct {
	Hub  *portalstomp.Hub
	Sess *portalstomp.Session
}

func NewFakeSTOMPSession() *FakeSTOMPSession {
	hub := portalstomp.NewHub()
	return &FakeSTOMPSession{Hub: hub, Sess: portalstomp.NewSession(hub)}
}

func (f *FakeSTOMPSession) Subscribe(topic, subID string) {
	f.Sess.HandleFrame("SUBSCRIBE", map[string]string{
		"id":          subID,
		"destination": topic,
	}, "")
}

func (f *FakeSTOMPSession) Unsubscribe(subID string) {
	f.Sess.HandleFrame("UNSUBSCRIBE", map[string]string{"id": subID}, "")
}

func (f *FakeSTOMPSession) Publish(topic string, body []byte) {
	f.Hub.Publish(topic, body)
}

func (f *FakeSTOMPSession) Close() {
	f.Sess.Close()
}